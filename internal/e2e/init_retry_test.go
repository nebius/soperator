package e2e

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRetryInitSucceedsFirstAttempt(t *testing.T) {
	calls := 0
	err := retryInit(context.Background(), 4, time.Millisecond, func(context.Context) error {
		calls++
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 1, calls)
}

func TestRetryInitSucceedsAfterTransientFailures(t *testing.T) {
	calls := 0
	err := retryInit(context.Background(), 4, time.Millisecond, func(context.Context) error {
		calls++
		if calls < 3 {
			return errors.New("502 Bad Gateway returned from github.com")
		}
		return nil
	})

	require.NoError(t, err)
	assert.Equal(t, 3, calls)
}

func TestRetryInitExhaustsAttempts(t *testing.T) {
	sentinel := errors.New("connection refused")
	calls := 0
	err := retryInit(context.Background(), 4, time.Millisecond, func(context.Context) error {
		calls++
		return sentinel
	})

	require.Error(t, err)
	assert.ErrorIs(t, err, sentinel)
	assert.Equal(t, 4, calls)
}

func TestRetryInitStopsOnCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	calls := 0
	err := retryInit(ctx, 4, time.Minute, func(context.Context) error {
		calls++
		cancel()
		return errors.New("context deadline exceeded")
	})

	assert.ErrorIs(t, err, context.Canceled)
	// Cancelled during the wait after the first failure, before a second attempt.
	assert.Equal(t, 1, calls)
}

func TestRetryInitClampsAttempts(t *testing.T) {
	calls := 0
	err := retryInit(context.Background(), 0, time.Millisecond, func(context.Context) error {
		calls++
		return errors.New("boom")
	})

	require.Error(t, err)
	assert.Equal(t, 1, calls)
}
