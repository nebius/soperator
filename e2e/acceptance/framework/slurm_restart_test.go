package framework

import (
	"context"
	"errors"
	"strings"
	"testing"
)

type restartJobRuntime struct {
	Runtime
	output     string
	queueState string
	err        error
	commands   []string
}

type restartJobScope struct {
	CommandScope
	output     string
	queueState string
	err        error
	commands   *[]string
}

func (r *restartJobRuntime) Jail() CommandScope {
	return restartJobScope{output: r.output, queueState: r.queueState, err: r.err, commands: &r.commands}
}

func (s restartJobScope) RunWithDefaultRetry(ctx context.Context, command string) (string, error) {
	*s.commands = append(*s.commands, command)
	switch {
	case strings.HasPrefix(command, "squeue "):
		return s.queueState, nil
	case strings.HasPrefix(command, "sacct "):
		return "123|COMPLETED|0:0|||", nil
	case command == "scontrol -o show job '123'":
		return s.output, s.err
	default:
		return "", errors.New("unexpected command: " + command)
	}
}

func TestJobStatusOnlyQueriesQueueAndAccounting(t *testing.T) {
	runtime := &restartJobRuntime{output: "JobId=123 Restarts=2"}
	info, err := NewSlurmClient(runtime).JobStatus(t.Context(), "123")
	if err != nil {
		t.Fatal(err)
	}
	if !info.CompletedSuccessfully() || info.Restarts != nil {
		t.Fatalf("unexpected terminal status: %+v", info)
	}
	if len(runtime.commands) != 2 || strings.Contains(strings.Join(runtime.commands, "\n"), "scontrol") {
		t.Fatalf("terminal status queried extra metadata: %v", runtime.commands)
	}
}

func TestJobInfoRestartCount(t *testing.T) {
	for _, tc := range []struct {
		name   string
		output string
		err    error
		known  bool
		count  int
	}{
		{name: "zero", output: "JobId=123 Restarts=0 BatchFlag=1", known: true},
		{name: "restarted", output: "JobId=123 Restarts=2", known: true, count: 2},
		{name: "missing", output: "JobId=123"},
		{name: "malformed", output: "Restarts=bad"},
		{name: "negative", output: "Restarts=-1"},
		{name: "unavailable", err: errors.New("job no longer retained")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			info, err := NewSlurmClient(&restartJobRuntime{output: tc.output, err: tc.err}).JobInfo(t.Context(), "123")
			if err != nil {
				t.Fatal(err)
			}
			if (info.Restarts != nil) != tc.known {
				t.Fatalf("restart count=%v, known=%t", info.Restarts, tc.known)
			}
			if tc.known && *info.Restarts != tc.count {
				t.Fatalf("restart count=%d, want %d", *info.Restarts, tc.count)
			}
			if !info.CompletedSuccessfully() {
				t.Fatal("restart metadata changed the ordinary success check")
			}
			if info.CompletedSuccessfullyWithoutRestarts() != (tc.known && tc.count == 0) {
				t.Fatal("unexpected completion-without-restarts result")
			}
			zero := 0
			info.Restarts = &zero
			info.SacctExit = "1:0"
			if info.CompletedSuccessfullyWithoutRestarts() {
				t.Fatal("failed job passed the completion check")
			}
		})
	}
}

func TestAssertJobRunningWithoutRestarts(t *testing.T) {
	for _, tc := range []struct {
		name       string
		queueState string
		output     string
		wantError  bool
	}{
		{name: "running without restarts", queueState: "RUNNING", output: "JobId=123 Restarts=0"},
		{name: "running after restart", queueState: "RUNNING", output: "JobId=123 Restarts=1", wantError: true},
		{name: "pending without restarts", queueState: "PENDING", output: "JobId=123 Restarts=0", wantError: true},
		{name: "running with unknown restarts", queueState: "RUNNING", output: "JobId=123", wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			runtime := &restartJobRuntime{queueState: tc.queueState, output: tc.output}
			err := NewSlurmClient(runtime).AssertJobRunningWithoutRestarts(t.Context(), "123")
			if (err != nil) != tc.wantError {
				t.Fatalf("assertion error=%v, want error=%t", err, tc.wantError)
			}
		})
	}
}
