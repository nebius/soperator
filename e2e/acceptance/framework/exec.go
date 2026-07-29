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

type WorkerKind string

const (
	WorkerAny WorkerKind = "any"
	WorkerCPU WorkerKind = "cpu"
	WorkerGPU WorkerKind = "gpu"
)

type Exec interface {
	// TODO: Split worker discovery into a WorkerInventory interface when the
	// acceptance framework has a broader cleanup pass. Exec should focus on
	// command execution, not cluster state inventory.
	AvailableWorkers(kind WorkerKind) []WorkerRef
	Kubectl() ArgsScope
	// Local returns a local process scope. Do not use it for kubectl commands;
	// use Kubectl instead so the explicit Kubernetes context is applied.
	Local() ArgsScope
	Controller() CommandScope
	Jail() CommandScope
	Worker(worker WorkerRef) CommandScope
	WorkerPod(worker WorkerRef) CommandScope
	WaitFor(ctx context.Context, description string, timeout, pollInterval time.Duration, condition func(context.Context) (bool, error)) error
	// TODO: Move Logf to a small Logger/Reporter interface together with the
	// WorkerInventory split so Exec stays limited to command execution.
	Logf(format string, args ...any)
}
