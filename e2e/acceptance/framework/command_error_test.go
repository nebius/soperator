package framework

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestKubectlExecNotStartedRequiresProofBeforeReplaying(t *testing.T) {
	for _, tc := range []struct {
		name   string
		stderr string
		output string
		want   bool
	}{
		{name: "missing container", stderr: `error: unable to upgrade connection: container not found ("sshd")`, want: true},
		{name: "missing pod", stderr: `Error from server (NotFound): pods "login-0" not found`, want: true},
		{name: "completed pod", stderr: "error: cannot exec into a container in a completed pod; current phase is Failed", want: true},
		{name: "backend unavailable before upgrade", stderr: "error: unable to upgrade connection: error dialing backend: dial tcp 10.0.0.1:10250: connection refused", want: true},
		{name: "remote command error", stderr: "sacctmgr: error: connection refused\ncommand terminated with exit code 1"},
		{name: "remote error looks like upgrade failure", stderr: "error: unable to upgrade connection: container not found\ncommand terminated with exit code 1"},
		{name: "remote container message", stderr: "container not found"},
		{name: "partial output", stderr: `error: unable to upgrade connection: container not found ("sshd")`, output: "submitted job 42\n"},
		{name: "uncertain reset", stderr: "error: read tcp 10.0.0.1:1234: connection reset by peer"},
		{name: "uncertain EOF", stderr: "error: unexpected EOF"},
		{name: "permission denied", stderr: "Error from server (Forbidden): pods is forbidden"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := &CommandError{Name: "kubectl", Args: []string{"--context", "dev", "exec", "login-0", "--", "sacctmgr"},
				Err: errors.New("exit status 1"), Stderr: tc.stderr}
			assert.Equal(t, tc.want, KubectlExecNotStarted(tc.output, fmt.Errorf("run in jail: %w", err)))
		})
	}
	message := `error: unable to upgrade connection: container not found ("sshd")`
	for _, err := range []error{
		nil,
		errors.New(message),
		&CommandError{Name: "ssh", Args: []string{"exec"}, Err: errors.New(message), Stderr: message},
		&CommandError{Name: "kubectl", Args: []string{"get", "pods"}, Err: errors.New("exit status 1"), Stderr: message},
		&CommandError{Name: "kubectl", Args: []string{"get", "pods", "--", "exec"}, Err: errors.New("exit status 1"), Stderr: message},
		&CommandError{Name: "kubectl", Args: []string{"exec", "login-0"}, Err: context.Canceled, Stderr: message},
		&CommandError{Name: "kubectl", Args: []string{"exec", "login-0"}, Err: context.DeadlineExceeded, Stderr: message},
		&CommandError{Name: "kubectl", Args: []string{"exec", "login-0", "--", message}, Err: errors.New("exit status 1"), Stderr: "unexpected EOF"},
	} {
		assert.False(t, KubectlExecNotStarted("", err), "%v", err)
	}
}
