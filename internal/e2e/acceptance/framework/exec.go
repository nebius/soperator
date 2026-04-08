package framework

import (
	"context"
	"time"
)

const (
	DefaultJailRetryAttempts = 5
	DefaultJailRetryDelay    = 10 * time.Second
)

type Exec interface {
	AnyWorker() (WorkerRef, error)
	ExecController(ctx context.Context, command string) (string, error)
	ExecJail(ctx context.Context, command string) (string, error)
	ExecJailWithRetry(ctx context.Context, command string, attempts int, delay time.Duration) (string, error)
	Run(ctx context.Context, name string, args ...string) (string, error)
	WaitFor(ctx context.Context, description string, timeout, pollInterval time.Duration, condition func(context.Context) (bool, error)) error
	Logf(format string, args ...any)
}

func ExecJailWithDefaultRetry(ctx context.Context, exec Exec, command string) (string, error) {
	return exec.ExecJailWithRetry(ctx, command, DefaultJailRetryAttempts, DefaultJailRetryDelay)
}
