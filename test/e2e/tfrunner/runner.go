package tfrunner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// Result holds the output from a terraform command.
type Result struct {
	Stdout   string
	Stderr   string
	ExitCode int
}

// Combined returns stdout and stderr combined.
func (r *Result) Combined() string {
	return strings.TrimSpace(r.Stdout + "\n" + r.Stderr)
}

// Runner executes terraform commands with graceful shutdown support.
type Runner struct {
	options  *Options
	patterns []compiledPattern
	t        *testing.T
}

// NewRunner creates a new terraform runner.
// The testing.T parameter is used for logging.
func NewRunner(options *Options, t *testing.T) (*Runner, error) {
	opts := options.WithDefaults()

	patterns, err := compileRetryPatterns(opts.RetryableErrors)
	if err != nil {
		return nil, fmt.Errorf("compile retry patterns: %w", err)
	}

	return &Runner{
		options:  opts,
		patterns: patterns,
		t:        t,
	}, nil
}

// Init runs terraform init.
func (r *Runner) Init(ctx context.Context) (*Result, error) {
	return r.Run(ctx, "init", "-input=false")
}

// Apply runs terraform apply.
func (r *Runner) Apply(ctx context.Context) (*Result, error) {
	args := []string{"apply", "-input=false", "-auto-approve"}
	if r.options.NoColor {
		args = append(args, "-no-color")
	}
	args = append(args, FormatTerraformVarsAsArgs(r.options.Vars)...)
	return r.Run(ctx, args...)
}

// Destroy runs terraform destroy.
func (r *Runner) Destroy(ctx context.Context) (*Result, error) {
	args := []string{"destroy", "-input=false", "-auto-approve"}
	if r.options.NoColor {
		args = append(args, "-no-color")
	}
	args = append(args, FormatTerraformVarsAsArgs(r.options.Vars)...)
	return r.Run(ctx, args...)
}

// WorkspaceSelectOrNew selects the given workspace, creating it if it doesn't exist.
func (r *Runner) WorkspaceSelectOrNew(ctx context.Context, name string) (*Result, error) {
	result, err := r.Run(ctx, "workspace", "select", name)
	if err == nil {
		return result, nil
	}

	// Try to create the workspace if select failed
	return r.Run(ctx, "workspace", "new", name)
}

// Run executes a terraform command with the given arguments.
func (r *Runner) Run(ctx context.Context, args ...string) (*Result, error) {
	return r.runWithRetry(ctx, args...)
}

// runWithRetry executes a command with retry logic.
func (r *Runner) runWithRetry(ctx context.Context, args ...string) (*Result, error) {
	var lastResult *Result
	var lastErr error

	maxAttempts := r.options.MaxRetries + 1
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		lastResult, lastErr = r.runCommand(ctx, args...)

		// Check context cancellation first
		if ctx.Err() != nil {
			return lastResult, lastErr
		}

		// Check if we should retry
		if lastErr != nil && len(r.patterns) > 0 && attempt < maxAttempts {
			match := matchRetryableError(r.patterns, lastResult.Stdout, lastResult.Stderr, lastErr)
			if match != "" {
				r.logf("Retryable error detected: %s (attempt %d/%d)", match, attempt, maxAttempts)
				select {
				case <-ctx.Done():
					return lastResult, lastErr
				case <-time.After(r.options.TimeBetweenRetries):
					continue
				}
			}
		}
		break
	}

	return lastResult, lastErr
}

// runCommand executes a single terraform command with graceful shutdown support.
// Note: We intentionally use exec.Command instead of exec.CommandContext because
// CommandContext sends SIGKILL on context cancellation, but we need SIGINT for
// graceful terraform shutdown to preserve state files.
func (r *Runner) runCommand(ctx context.Context, args ...string) (*Result, error) {
	cmd := exec.Command(r.options.TerraformBinary, args...) //nolint:gosec,noctx
	cmd.Dir = r.options.TerraformDir

	// Set up environment
	cmd.Env = os.Environ()
	for k, v := range r.options.EnvVars {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Capture output
	var stdoutBuf, stderrBuf bytes.Buffer
	stdoutWriter := io.Writer(&stdoutBuf)
	stderrWriter := io.Writer(&stderrBuf)

	if r.options.Stdout != nil {
		stdoutWriter = io.MultiWriter(&stdoutBuf, r.options.Stdout)
	}
	if r.options.Stderr != nil {
		stderrWriter = io.MultiWriter(&stderrBuf, r.options.Stderr)
	}

	cmd.Stdout = stdoutWriter
	cmd.Stderr = stderrWriter

	r.logf("Running: %s %s", r.options.TerraformBinary, strings.Join(args, " "))

	if err := cmd.Start(); err != nil {
		return &Result{ExitCode: -1}, fmt.Errorf("start terraform: %w", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case err := <-done:
		// Command completed normally
		return r.buildResult(&stdoutBuf, &stderrBuf, err)

	case <-ctx.Done():
		// Context cancelled - initiate graceful shutdown
		r.logf("Context cancelled, sending SIGINT for graceful shutdown")

		if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
			r.logf("Send SIGINT: %v", err)
		}

		// Wait for graceful shutdown with timeout
		select {
		case err := <-done:
			r.logf("Terraform exited gracefully after SIGINT")
			return r.buildResult(&stdoutBuf, &stderrBuf, err)

		case <-time.After(r.options.GracefulShutdownTimeout):
			r.logf("Graceful shutdown timeout exceeded, sending SIGKILL")
			if err := cmd.Process.Kill(); err != nil {
				r.logf("Send SIGKILL: %v", err)
			}
			<-done // Wait for process to exit
			result := r.buildResultValues(&stdoutBuf, &stderrBuf, -1)
			return result, fmt.Errorf("terraform killed after graceful shutdown timeout: %w", ctx.Err())
		}
	}
}

// buildResult creates a Result from command output and error.
func (r *Runner) buildResult(stdout, stderr *bytes.Buffer, err error) (*Result, error) {
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = -1
		}
	}

	result := r.buildResultValues(stdout, stderr, exitCode)

	if err != nil {
		return result, fmt.Errorf("terraform command failed with exit code %d: %w", exitCode, err)
	}
	return result, nil
}

// buildResultValues creates a Result from raw values.
func (r *Runner) buildResultValues(stdout, stderr *bytes.Buffer, exitCode int) *Result {
	return &Result{
		Stdout:   stdout.String(),
		Stderr:   stderr.String(),
		ExitCode: exitCode,
	}
}

// logf logs a message using the testing.T logger.
func (r *Runner) logf(format string, args ...any) {
	if r.t != nil {
		r.t.Logf("[tfrunner] "+format, args...)
	}
}
