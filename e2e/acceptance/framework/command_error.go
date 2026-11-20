package framework

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// CommandError preserves diagnostics separately from argv so retry decisions
// cannot be influenced by a command's payload. Runtime adapters may return it
// without changing the CommandScope or ArgsScope contracts. Opaque errors do
// not establish a retryable command failure or a definite submission rejection.
type CommandError struct {
	Name   string
	Args   []string
	Err    error
	Stderr string
}

func (e *CommandError) Error() string {
	message := fmt.Sprintf("%s %s: %v", e.Name, strings.Join(e.Args, " "), e.Err)
	if e.Stderr != "" {
		message += ": " + e.Stderr
	}
	return message
}

func (e *CommandError) Unwrap() error { return e.Err }

// KubectlExecNotStarted reports failures that prove an exec command never ran.
// Connection loss after execution starts must not replay commands that change state.
func KubectlExecNotStarted(output string, err error) bool {
	if err == nil || strings.TrimSpace(output) != "" || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	var command *CommandError
	if !errors.As(err, &command) || command.Name != "kubectl" {
		return false
	}
	var execCommand bool
	for _, arg := range command.Args {
		if arg == "--" {
			break
		}
		if arg == "exec" {
			execCommand = true
			break
		}
	}
	if !execCommand {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(command.Stderr))
	if strings.Contains(message, "command terminated with exit") {
		return false
	}
	return strings.HasPrefix(message, "error: unable to upgrade connection:") ||
		strings.HasPrefix(message, "error: cannot exec into a container in a completed pod;") ||
		(strings.HasPrefix(message, "error from server (notfound): pods \"") && strings.HasSuffix(message, "\" not found"))
}

func commandFailureText(err error) string {
	var command *CommandError
	if errors.As(err, &command) {
		return strings.ToLower(fmt.Sprint(command.Err) + "\n" + command.Stderr)
	}
	return ""
}
