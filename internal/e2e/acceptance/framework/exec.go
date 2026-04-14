package framework

import (
	"context"
	"time"
)

const (
	DefaultRetryAttempts = 3
	DefaultRetryDelay    = 5 * time.Second
)

type Exec interface {
	AnyWorker() (WorkerRef, error)
	AnyGPUWorker() (WorkerRef, error)
	ExecController(ctx context.Context, command string) (string, error)
	ExecJail(ctx context.Context, command string) (string, error)
	ExecJailWithRetry(ctx context.Context, command string, attempts int, delay time.Duration) (string, error)
	Run(ctx context.Context, name string, args ...string) (string, error)
	RunWithRetry(ctx context.Context, attempts int, delay time.Duration, name string, args ...string) (string, error)
	WaitFor(ctx context.Context, description string, timeout, pollInterval time.Duration, condition func(context.Context) (bool, error)) error
	Logf(format string, args ...any)
}

func ExecJailWithDefaultRetry(ctx context.Context, exec Exec, command string) (string, error) {
	return exec.ExecJailWithRetry(ctx, command, DefaultRetryAttempts, DefaultRetryDelay)
}

func RunWithDefaultRetry(ctx context.Context, exec Exec, name string, args ...string) (string, error) {
	return exec.RunWithRetry(ctx, DefaultRetryAttempts, DefaultRetryDelay, name, args...)
}
