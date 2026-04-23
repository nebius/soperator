package framework

import (
	"context"
	"time"
)

const (
	DefaultRetryAttempts = 5
	DefaultRetryDelay    = 10 * time.Second
)

type CommandScope interface {
	Run(ctx context.Context, command string) (string, error)
	RunWithRetry(ctx context.Context, command string, attempts int, delay time.Duration) (string, error)
	RunWithDefaultRetry(ctx context.Context, command string) (string, error)
}

type Exec interface {
	AvailableWorkers() []WorkerPodRef
	AvailableGPUWorkers() []WorkerPodRef
	Controller() CommandScope
	Jail() CommandScope
	Worker(worker string) CommandScope
	Run(ctx context.Context, name string, args ...string) (string, error)
	RunWithRetry(ctx context.Context, attempts int, delay time.Duration, name string, args ...string) (string, error)
	RunWithDefaultRetry(ctx context.Context, name string, args ...string) (string, error)
	WaitFor(ctx context.Context, description string, timeout, pollInterval time.Duration, condition func(context.Context) (bool, error)) error
	Logf(format string, args ...any)
}
