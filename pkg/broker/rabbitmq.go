package broker

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"github.com/practicum/gophprofile/internal/observability"
	amqp "github.com/rabbitmq/amqp091-go"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

const (
	RoutingKeyUploaded = "avatar.uploaded"
	RoutingKeyDelete   = "avatar.delete"
	QueueProcess       = "avatars.process"
	QueueDelete        = "avatars.delete"
)

type MessageHandler func(ctx context.Context, body []byte) error

type RabbitMQ struct {
	conn     *amqp.Connection
	channel  *amqp.Channel
	exchange string
	url      string
}

func NewRabbitMQ(url, exchange string) (*RabbitMQ, error) {
	r := &RabbitMQ{url: url, exchange: exchange}
	if err := r.connect(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *RabbitMQ) connect() error {
	conn, err := amqp.Dial(r.url)
	if err != nil {
		return fmt.Errorf("dial rabbitmq: %w", err)
	}
	ch, err := conn.Channel()
	if err != nil {
		_ = conn.Close()
		return fmt.Errorf("open channel: %w", err)
	}

	if err := ch.ExchangeDeclare(r.exchange, "topic", true, false, false, false, nil); err != nil {
		_ = ch.Close()
		_ = conn.Close()
		return fmt.Errorf("declare exchange: %w", err)
	}

	r.conn = conn
	r.channel = ch
	return nil
}

func (r *RabbitMQ) Ping(_ context.Context) error {
	if r.conn == nil || r.conn.IsClosed() {
		return fmt.Errorf("rabbitmq connection closed")
	}
	return nil
}

func (r *RabbitMQ) Close() error {
	var err error
	if r.channel != nil {
		err = r.channel.Close()
	}
	if r.conn != nil {
		if e := r.conn.Close(); e != nil && err == nil {
			err = e
		}
	}
	return err
}

func (r *RabbitMQ) Publish(ctx context.Context, routingKey string, payload any) error {
	tracer := otel.Tracer("gophprofile/broker")
	ctx, span := tracer.Start(ctx, "broker.publish",
		trace.WithSpanKind(trace.SpanKindProducer),
		trace.WithAttributes(
			attribute.String("messaging.system", "rabbitmq"),
			attribute.String("messaging.destination", r.exchange),
			attribute.String("messaging.rabbitmq.routing_key", routingKey),
		),
	)
	defer span.End()

	body, err := json.Marshal(payload)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}

	headers := amqp.Table{}
	otel.GetTextMapPropagator().Inject(ctx, amqpHeaderCarrier(headers))

	err = r.channel.PublishWithContext(ctx, r.exchange, routingKey, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now().UTC(),
		Body:         body,
		MessageId:    extractMessageID(payload),
		Headers:      headers,
	})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return err
	}
	return nil
}

func extractMessageID(payload any) string {
	switch v := payload.(type) {
	case interface{ GetMessageID() string }:
		return v.GetMessageID()
	default:
		type mid struct {
			MessageID string `json:"message_id"`
		}
		b, err := json.Marshal(payload)
		if err != nil {
			return ""
		}
		var m mid
		_ = json.Unmarshal(b, &m)
		return m.MessageID
	}
}

func (r *RabbitMQ) SetupQueues() error {
	bindings := []struct {
		queue string
		key   string
	}{
		{QueueProcess, RoutingKeyUploaded},
		{QueueDelete, RoutingKeyDelete},
	}

	for _, b := range bindings {
		_, err := r.channel.QueueDeclare(b.queue, true, false, false, false, nil)
		if err != nil {
			return err
		}
		if err := r.channel.QueueBind(b.queue, b.key, r.exchange, false, nil); err != nil {
			return err
		}
	}
	return nil
}

func (r *RabbitMQ) Consume(ctx context.Context, queue string, handler MessageHandler) error {
	if err := r.channel.Qos(1, 0, false); err != nil {
		return err
	}

	deliveries, err := r.channel.Consume(queue, "", false, false, false, false, nil)
	if err != nil {
		return err
	}

	tracer := otel.Tracer("gophprofile/broker")

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case d, ok := <-deliveries:
				if !ok {
					slog.Warn("broker delivery channel closed", "queue", queue)
					return
				}

				msgCtx := otel.GetTextMapPropagator().Extract(ctx, amqpHeaderCarrier(d.Headers))
				msgCtx, span := tracer.Start(msgCtx, "broker.consume "+queue,
					trace.WithSpanKind(trace.SpanKindConsumer),
					trace.WithAttributes(
						attribute.String("messaging.system", "rabbitmq"),
						attribute.String("messaging.destination", queue),
						attribute.String("messaging.message_id", d.MessageId),
					),
				)

				err := handler(msgCtx, d.Body)
				if err != nil {
					span.RecordError(err)
					span.SetStatus(codes.Error, err.Error())
					observability.LoggerFromContext(msgCtx).Error("broker handler error", "queue", queue, "error", err)
					span.End()
					time.Sleep(time.Second)
					_ = d.Nack(false, true)
					continue
				}
				span.End()
				_ = d.Ack(false)
			}
		}
	}()
	return nil
}

func RetryWithBackoff(ctx context.Context, attempts int, base time.Duration, fn func() error) error {
	var err error
	delay := base
	for i := 0; i < attempts; i++ {
		if err = fn(); err == nil {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
			delay *= 2
		}
	}
	return err
}
