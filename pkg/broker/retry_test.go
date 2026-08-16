package broker_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/practicum/gophprofile/pkg/broker"
	"github.com/stretchr/testify/require"
)

func TestRetryWithBackoff_Success(t *testing.T) {
	calls := 0
	err := broker.RetryWithBackoff(context.Background(), 3, time.Millisecond, func() error {
		calls++
		if calls < 2 {
			return errors.New("tmp")
		}
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 2, calls)
}

func TestRetryWithBackoff_Exhausted(t *testing.T) {
	err := broker.RetryWithBackoff(context.Background(), 2, time.Millisecond, func() error {
		return errors.New("fail")
	})
	require.Error(t, err)
}

func TestRetryWithBackoff_Canceled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := broker.RetryWithBackoff(ctx, 5, time.Second, func() error {
		return errors.New("fail")
	})
	require.Error(t, err)
}
