package circuitbreaker

import (
	"errors"
	"time"

	"github.com/practicum/gophprofile/internal/observability"
	"github.com/practicum/gophprofile/internal/repository"
	"github.com/sony/gobreaker/v2"
)

// ErrOpen is returned when the circuit is open.
var ErrOpen = gobreaker.ErrOpenState

// Breaker wraps gobreaker with Prometheus metrics.
type Breaker struct {
	name string
	cb   *gobreaker.CircuitBreaker[any]
}

// Settings for creating a breaker.
type Settings struct {
	Name        string
	MaxRequests uint32
	Interval    time.Duration
	Timeout     time.Duration
	Failures    uint32 // consecutive failures to trip; default 5
}

// New creates a named circuit breaker with metrics.
func New(s Settings) *Breaker {
	if s.Name == "" {
		s.Name = "default"
	}
	if s.MaxRequests == 0 {
		s.MaxRequests = 1
	}
	if s.Timeout <= 0 {
		s.Timeout = 30 * time.Second
	}
	if s.Failures == 0 {
		s.Failures = 5
	}

	b := &Breaker{name: s.Name}
	b.cb = gobreaker.NewCircuitBreaker[any](gobreaker.Settings{
		Name:        s.Name,
		MaxRequests: s.MaxRequests,
		Interval:    s.Interval,
		Timeout:     s.Timeout,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= s.Failures
		},
		IsSuccessful: isSuccessful,
		OnStateChange: func(name string, from, to gobreaker.State) {
			observability.CircuitBreakerState.WithLabelValues(name).Set(stateValue(to))
			if to == gobreaker.StateOpen {
				observability.CircuitBreakerTrips.WithLabelValues(name).Inc()
			}
		},
	})
	observability.CircuitBreakerState.WithLabelValues(s.Name).Set(stateValue(gobreaker.StateClosed))
	return b
}

// Execute runs fn under the circuit breaker.
func (b *Breaker) Execute(fn func() error) error {
	_, err := b.cb.Execute(func() (any, error) {
		return nil, fn()
	})
	return err
}

// ExecuteValue runs fn that returns a value under the circuit breaker.
func ExecuteValue[T any](b *Breaker, fn func() (T, error)) (T, error) {
	var zero T
	v, err := b.cb.Execute(func() (any, error) {
		return fn()
	})
	if err != nil {
		return zero, err
	}
	if v == nil {
		return zero, nil
	}
	return v.(T), nil
}

// Name returns the breaker name.
func (b *Breaker) Name() string { return b.name }

// State returns the current state string (closed/half-open/open).
func (b *Breaker) State() string {
	return b.cb.State().String()
}

// IsOpen reports whether the circuit is open.
func (b *Breaker) IsOpen() bool {
	return b.cb.State() == gobreaker.StateOpen
}

func stateValue(s gobreaker.State) float64 {
	switch s {
	case gobreaker.StateClosed:
		return 0
	case gobreaker.StateHalfOpen:
		return 1
	case gobreaker.StateOpen:
		return 2
	default:
		return -1
	}
}

func isSuccessful(err error) bool {
	if err == nil {
		return true
	}
	// Business / not-found errors must not trip the breaker.
	if errors.Is(err, repository.ErrNotFound) {
		return true
	}
	return false
}
