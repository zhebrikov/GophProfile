package circuitbreaker

import (
	"context"
)

// eventPublisher is the message publish surface wrapped by the breaker.
type eventPublisher interface {
	Publish(ctx context.Context, routingKey string, payload any) error
	Ping(ctx context.Context) error
}

// PublisherBreaker wraps Publish/Ping with a circuit breaker.
type PublisherBreaker struct {
	inner eventPublisher
	cb    *Breaker
}

// WrapPublisher returns a publisher wrapper protected by breaker name "broker".
func WrapPublisher(inner eventPublisher, cb *Breaker) *PublisherBreaker {
	if cb == nil {
		cb = New(Settings{Name: "broker"})
	}
	return &PublisherBreaker{inner: inner, cb: cb}
}

func (p *PublisherBreaker) Publish(ctx context.Context, routingKey string, payload any) error {
	return p.cb.Execute(func() error {
		return p.inner.Publish(ctx, routingKey, payload)
	})
}

func (p *PublisherBreaker) Ping(ctx context.Context) error {
	if p.cb.IsOpen() {
		return ErrOpen
	}
	return p.cb.Execute(func() error {
		return p.inner.Ping(ctx)
	})
}

// Breaker exposes the underlying breaker for health checks.
func (p *PublisherBreaker) Breaker() *Breaker { return p.cb }
