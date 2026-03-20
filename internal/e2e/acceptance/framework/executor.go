package framework

import (
	"context"
	"time"
)

type Executor interface {
	AnyWorker() (WorkerRef, error)
	ExecController(ctx context.Context, command string) (string, error)
	ExecJail(ctx context.Context, command string) (string, error)
	ExecJailWithRetry(ctx context.Context, command string, attempts int, delay time.Duration) (string, error)
	Run(ctx context.Context, name string, args ...string) (string, error)
	WaitFor(ctx context.Context, description string, timeout, pollInterval time.Duration, condition func(context.Context) (bool, error)) error
	Logf(format string, args ...any)
}

func ShellQuote(value string) string {
	result := "'"
	for _, r := range value {
		if r == '\'' {
			result += `'"'"'`
			continue
		}
		result += string(r)
	}
	result += "'"
	return result
}
