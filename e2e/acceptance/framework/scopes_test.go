package framework

import (
	"context"
	"errors"
	"reflect"
	"testing"
)

func TestNewArgsScopeRetries(t *testing.T) {
	var calls int
	var gotArgs []string
	scope := NewArgsScope(func(ctx context.Context, args ...string) (string, error) {
		calls++
		gotArgs = append([]string(nil), args...)
		if calls == 1 {
			return "", errors.New("temporary")
		}
		return "ok", nil
	})

	out, err := scope.RunWithRetry(t.Context(), 2, 0, "get", "pods")
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
}

func TestNewCommandScopeRetries(t *testing.T) {
	var calls int
	var gotCommand string
	scope := NewCommandScope(func(ctx context.Context, command string) (string, error) {
		calls++
		gotCommand = command
		if calls == 1 {
			return "", errors.New("temporary")
		}
		return "ok", nil
	})

	out, err := scope.RunWithRetry(t.Context(), "sinfo", 2, 0)
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
