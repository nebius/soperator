package framework

import (
	"context"
	"time"
)

const (
	DefaultCommandTimeout = 10 * time.Minute
	DefaultRetryAttempts  = 5
	DefaultRetryDelay     = 10 * time.Second
)

type CommandScope interface {
	Run(ctx context.Context, command string) (string, error)
	RunWithRetry(ctx context.Context, command string, attempts int, delay, timeout time.Duration) (string, error)
	RunWithDefaultRetry(ctx context.Context, command string) (string, error)
}

type ArgsScope interface {
	Run(ctx context.Context, args ...string) (string, error)
	RunWithRetry(ctx context.Context, attempts int, delay, timeout time.Duration, args ...string) (string, error)
	RunWithDefaultRetry(ctx context.Context, args ...string) (string, error)
}

type Exec interface {
	Kubectl() ArgsScope
	// Local returns a local process scope. Do not use it for kubectl commands;
	// use Kubectl instead so the explicit Kubernetes context is applied.
	Local() ArgsScope
	Controller() CommandScope
	Jail() CommandScope
	Worker(worker WorkerInfo) CommandScope
	WorkerPod(pod WorkerPodInfo) CommandScope
}

type Waiter interface {
	WaitFor(ctx context.Context, description string, timeout, pollInterval time.Duration, condition func(context.Context) (bool, error)) error
}

type Logger interface {
	Logf(format string, args ...any)
}

type Runtime interface {
	Exec
	Waiter
	Logger
}
