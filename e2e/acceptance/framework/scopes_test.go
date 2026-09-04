package framework

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"
)

func TestNewArgsScopeRetries(t *testing.T) {
	const commandTimeout = 2 * time.Minute

	var calls int
	var gotArgs []string
	var deadlines []time.Time
	scope := NewArgsScope(func(ctx context.Context, args ...string) (string, error) {
		calls++
		gotArgs = append([]string(nil), args...)
		deadlines = append(deadlines, requireContextTimeout(t, ctx, commandTimeout))
		if calls == 1 {
			return "", errors.New("temporary")
		}
		return "ok", nil
	})

	out, err := scope.RunWithRetry(t.Context(), 2, 0, commandTimeout, "get", "pods")
	if err != nil {
		t.Fatalf("RunWithRetry returned error: %v", err)
	}
	if out != "ok" {
		t.Fatalf("out=%q, want ok", out)
	}
	if calls != 2 {
		t.Fatalf("calls=%d, want 2", calls)
	}
	if want := []string{"get", "pods"}; !reflect.DeepEqual(gotArgs, want) {
		t.Fatalf("args=%v, want %v", gotArgs, want)
	}
	if !deadlines[1].After(deadlines[0]) {
		t.Fatalf("retry deadline %s is not after first attempt deadline %s", deadlines[1], deadlines[0])
	}
}

func TestNewCommandScopeRetries(t *testing.T) {
	const commandTimeout = 2 * time.Minute

	var calls int
	var gotCommand string
	scope := NewCommandScope(func(ctx context.Context, command string) (string, error) {
		calls++
		gotCommand = command
		requireContextTimeout(t, ctx, commandTimeout)
		if calls == 1 {
			return "", errors.New("temporary")
		}
		return "ok", nil
	})

	out, err := scope.RunWithRetry(t.Context(), "sinfo", 2, 0, commandTimeout)
	if err != nil {
		t.Fatalf("RunWithRetry returned error: %v", err)
	}
	if out != "ok" {
		t.Fatalf("out=%q, want ok", out)
	}
	if calls != 2 {
		t.Fatalf("calls=%d, want 2", calls)
	}
	if gotCommand != "sinfo" {
		t.Fatalf("command=%q, want sinfo", gotCommand)
	}
}

func TestRunUsesDefaultCommandTimeout(t *testing.T) {
	scope := NewArgsScope(func(ctx context.Context, args ...string) (string, error) {
		requireContextTimeout(t, ctx, DefaultCommandTimeout)
		return "ok", nil
	})

	if _, err := scope.Run(t.Context(), "get", "pods"); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
}

func TestRunWithDefaultRetryUsesDefaultCommandTimeout(t *testing.T) {
	scope := NewCommandScope(func(ctx context.Context, command string) (string, error) {
		requireContextTimeout(t, ctx, DefaultCommandTimeout)
		return "ok", nil
	})

	out, err := scope.RunWithDefaultRetry(t.Context(), "sinfo")
	if err != nil {
		t.Fatalf("RunWithDefaultRetry returned error: %v", err)
	}
	if out != "ok" {
		t.Fatalf("out=%q, want ok", out)
	}
}

func TestNestedScopePreservesCustomTimeout(t *testing.T) {
	const commandTimeout = 30 * time.Minute

	inner := NewArgsScope(func(ctx context.Context, args ...string) (string, error) {
		requireContextTimeout(t, ctx, commandTimeout)
		return "ok", nil
	})
	outer := NewArgsScope(func(ctx context.Context, args ...string) (string, error) {
		return inner.Run(ctx, args...)
	})

	if _, err := outer.RunWithRetry(t.Context(), 1, 0, commandTimeout, "operation", "wait"); err != nil {
		t.Fatalf("RunWithRetry returned error: %v", err)
	}
}

func TestRunPreservesExistingDeadline(t *testing.T) {
	const parentTimeout = time.Minute

	ctx, cancel := context.WithTimeout(t.Context(), parentTimeout)
	defer cancel()

	scope := NewArgsScope(func(ctx context.Context, args ...string) (string, error) {
		requireContextTimeout(t, ctx, parentTimeout)
		return "ok", nil
	})

	if _, err := scope.Run(ctx, "operation", "wait"); err != nil {
		t.Fatalf("Run returned error: %v", err)
	}
}

func requireContextTimeout(t *testing.T, ctx context.Context, want time.Duration) time.Time {
	t.Helper()

	deadline, ok := ctx.Deadline()
	if !ok {
		t.Fatal("command context has no deadline")
	}
	remaining := time.Until(deadline)
	if remaining > want || remaining < want-time.Second {
		t.Fatalf("command timeout=%s, want approximately %s", remaining, want)
	}
	return deadline
}
