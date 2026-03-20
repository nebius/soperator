package e2e

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func RunAcceptance(ctx context.Context, _ Config) error {
	cmd := exec.CommandContext(
		ctx,
		"go", "run", "github.com/onsi/ginkgo/v2/ginkgo",
		"--procs=2",
		"--tags=acceptance",
		"--timeout=90m",
		"--v",
		"--show-node-events",
		"./internal/e2e/acceptance",
	)
	cmd.Dir = repoRoot()
	cmd.Env = os.Environ()
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("run acceptance suite: %w", err)
	}

	return nil
}

func repoRoot() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "."
	}

	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}
