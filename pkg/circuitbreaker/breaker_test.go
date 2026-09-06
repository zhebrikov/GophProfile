package circuitbreaker

import (
	"errors"
	"testing"
	"time"

	"github.com/practicum/gophprofile/internal/repository"
	"github.com/stretchr/testify/require"
)

func TestBreakerOpensAfterFailures(t *testing.T) {
	b := New(Settings{
		Name:        "test",
		MaxRequests: 1,
		Timeout:     100 * time.Millisecond,
		Failures:    3,
	})

	fail := errors.New("boom")
	for i := 0; i < 3; i++ {
		err := b.Execute(func() error { return fail })
		require.ErrorIs(t, err, fail)
	}

	err := b.Execute(func() error { return nil })
	require.ErrorIs(t, err, ErrOpen)
	require.True(t, b.IsOpen())
	require.Equal(t, "open", b.State())
}

func TestBreakerNotFoundDoesNotTrip(t *testing.T) {
	b := New(Settings{
		Name:     "test-nf",
		Failures: 2,
		Timeout:  time.Second,
	})

	for i := 0; i < 5; i++ {
		err := b.Execute(func() error { return repository.ErrNotFound })
		require.ErrorIs(t, err, repository.ErrNotFound)
	}
	require.False(t, b.IsOpen())
	require.Equal(t, "closed", b.State())
}

func TestBreakerHalfOpenRecovery(t *testing.T) {
	b := New(Settings{
		Name:        "test-recover",
		MaxRequests: 1,
		Timeout:     50 * time.Millisecond,
		Failures:    2,
	})

	fail := errors.New("down")
	require.Error(t, b.Execute(func() error { return fail }))
	require.Error(t, b.Execute(func() error { return fail }))
	require.True(t, b.IsOpen())

	time.Sleep(60 * time.Millisecond)
	require.NoError(t, b.Execute(func() error { return nil }))
	require.Equal(t, "closed", b.State())
}

func TestExecuteValue(t *testing.T) {
	b := New(Settings{Name: "test-val", Failures: 5})
	v, err := ExecuteValue(b, func() (int, error) { return 42, nil })
	require.NoError(t, err)
	require.Equal(t, 42, v)
}
