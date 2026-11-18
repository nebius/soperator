package framework

import (
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

func commandFailureText(err error) string {
	var command *CommandError
	if errors.As(err, &command) {
		return strings.ToLower(fmt.Sprint(command.Err) + "\n" + command.Stderr)
	}
	return ""
}
