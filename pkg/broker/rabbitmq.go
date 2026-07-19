package broker

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"time"

	amqp "github.com/rabbitmq/amqp091-go"
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
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	return r.channel.PublishWithContext(ctx, r.exchange, routingKey, false, false, amqp.Publishing{
		ContentType:  "application/json",
		DeliveryMode: amqp.Persistent,
		Timestamp:    time.Now().UTC(),
		Body:         body,
		MessageId:    extractMessageID(payload),
	})
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

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case d, ok := <-deliveries:
				if !ok {
					log.Printf("broker: delivery channel closed for queue %s", queue)
					return
				}
				if err := handler(ctx, d.Body); err != nil {
					log.Printf("broker: handler error on %s: %v", queue, err)
					// Requeue only transient failures; avoid tight loops by delaying.
					time.Sleep(time.Second)
					_ = d.Nack(false, true)
					continue
				}
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
