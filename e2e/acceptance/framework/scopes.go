package framework

import (
	"context"
	"time"
)

type argsScope struct {
	run func(context.Context, ...string) (string, error)
}

// NewArgsScope returns an ArgsScope backed by the provided command runner.
func NewArgsScope(run func(context.Context, ...string) (string, error)) ArgsScope {
	return argsScope{run: run}
}

func (s argsScope) Run(ctx context.Context, args ...string) (string, error) {
	return runWithDefaultTimeout(ctx, func(commandCtx context.Context) (string, error) {
		return s.run(commandCtx, args...)
	})
}

func (s argsScope) RunWithRetry(
	ctx context.Context,
	attempts int,
	delay time.Duration,
	timeout time.Duration,
	args ...string,
) (string, error) {
	return retry(ctx, attempts, delay, func(attemptCtx context.Context) (string, error) {
		return runWithTimeout(attemptCtx, timeout, func(commandCtx context.Context) (string, error) {
			return s.run(commandCtx, args...)
		})
	})
}

func (s argsScope) RunWithDefaultRetry(ctx context.Context, args ...string) (string, error) {
	return s.RunWithRetry(ctx, DefaultRetryAttempts, DefaultRetryDelay, DefaultCommandTimeout, args...)
}

type commandScope struct {
	run func(context.Context, string) (string, error)
}

// NewCommandScope returns a CommandScope backed by the provided command runner.
func NewCommandScope(run func(context.Context, string) (string, error)) CommandScope {
	return commandScope{run: run}
}

func (s commandScope) Run(ctx context.Context, command string) (string, error) {
	return runWithDefaultTimeout(ctx, func(commandCtx context.Context) (string, error) {
		return s.run(commandCtx, command)
	})
}

func (s commandScope) RunWithRetry(
	ctx context.Context,
	command string,
	attempts int,
	delay time.Duration,
	timeout time.Duration,
) (string, error) {
	return retry(ctx, attempts, delay, func(attemptCtx context.Context) (string, error) {
		return runWithTimeout(attemptCtx, timeout, func(commandCtx context.Context) (string, error) {
			return s.run(commandCtx, command)
		})
	})
}

func (s commandScope) RunWithDefaultRetry(ctx context.Context, command string) (string, error) {
	return s.RunWithRetry(ctx, command, DefaultRetryAttempts, DefaultRetryDelay, DefaultCommandTimeout)
}

func runWithDefaultTimeout(ctx context.Context, run func(context.Context) (string, error)) (string, error) {
	if _, ok := ctx.Deadline(); ok {
		return run(ctx)
	}

	return runWithTimeout(ctx, DefaultCommandTimeout, run)
}

func runWithTimeout(
	ctx context.Context,
	timeout time.Duration,
	run func(context.Context) (string, error),
) (string, error) {
	commandCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return run(commandCtx)
}

func retry(ctx context.Context, attempts int, delay time.Duration, run func(context.Context) (string, error)) (string, error) {
	if attempts < 1 {
		attempts = 1
	}

	var lastErr error
	var out string
	for attempt := 1; attempt <= attempts; attempt++ {
		out, lastErr = run(ctx)
		if lastErr == nil {
			return out, nil
		}
		if attempt == attempts {
			break
		}

		select {
		case <-ctx.Done():
			return out, ctx.Err()
		case <-time.After(delay):
		}
	}

	return out, lastErr
}
