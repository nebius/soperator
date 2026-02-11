package tfrunner

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
)

type Options struct {
	Dir      string
	VarFiles []string
	EnvVars  map[string]string
}

type Runner struct {
	opts Options
}

func New(opts Options) *Runner {
	return &Runner{opts: opts}
}

// Run executes a terraform command with the given arguments.
// Stdout is captured and returned while also being streamed to os.Stdout.
// Stderr is streamed directly to os.Stderr.
func (r *Runner) Run(args ...string) (string, error) {
	args = append(args, "-no-color")

	cmd := exec.CommandContext(context.Background(), "terraform", args...)
	cmd.Dir = r.opts.Dir

	if len(r.opts.EnvVars) > 0 {
		var env []string
		for k, v := range r.opts.EnvVars {
			env = append(env, k+"="+v)
		}
		cmd.Env = env
	}

	var buf bytes.Buffer
	cmd.Stdout = io.MultiWriter(os.Stdout, &buf)
	cmd.Stderr = os.Stderr

	err := cmd.Run()
	if err != nil {
		return buf.String(), fmt.Errorf("terraform %s: %w", strings.Join(args, " "), err)
	}
	return buf.String(), nil
}

func (r *Runner) Init() error {
	_, err := r.Run("init", "-upgrade=false")
	return err
}

func (r *Runner) Apply() error {
	args := []string{"apply", "-input=false", "-auto-approve"}
	for _, vf := range r.opts.VarFiles {
		args = append(args, "-var-file="+vf)
	}
	_, err := r.Run(args...)
	return err
}

func (r *Runner) Destroy() error {
	args := []string{"destroy", "-auto-approve", "-input=false"}
	for _, vf := range r.opts.VarFiles {
		args = append(args, "-var-file="+vf)
	}
	_, err := r.Run(args...)
	return err
}

func (r *Runner) WorkspaceSelectOrNew(name string) error {
	out, err := r.Run("workspace", "list")
	if err != nil {
		return err
	}
	if isExistingWorkspace(out, name) {
		_, err = r.Run("workspace", "select", name)
	} else {
		_, err = r.Run("workspace", "new", name)
	}
	return err
}

func isExistingWorkspace(out, name string) bool {
	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)
		// workspace list output prefixes active workspace with "* "
		trimmed = strings.TrimPrefix(trimmed, "* ")
		if trimmed == name {
			return true
		}
	}
	return false
}
