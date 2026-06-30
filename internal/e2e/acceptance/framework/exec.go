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

type ArgsScope interface {
	Run(ctx context.Context, args ...string) (string, error)
	RunWithRetry(ctx context.Context, attempts int, delay time.Duration, args ...string) (string, error)
	RunWithDefaultRetry(ctx context.Context, args ...string) (string, error)
}

type Exec interface {
	AvailableWorkers() []WorkerPodRef
	AvailableGPUWorkers() []WorkerPodRef
	Kubectl() ArgsScope
	// Local returns a local process scope. Do not use it for kubectl commands;
	// use Kubectl instead so the explicit Kubernetes context is applied.
	Local() ArgsScope
	Controller() CommandScope
	Jail() CommandScope
	Worker(worker string) CommandScope
	WaitFor(ctx context.Context, description string, timeout, pollInterval time.Duration, condition func(context.Context) (bool, error)) error
	Logf(format string, args ...any)
}
