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
		"go", "test",
		"-count=1",
		"-tags", "acceptance",
		"-timeout", "90m",
		"-v",
		"./internal/e2e/acceptance",
		"-args",
		"-ginkgo.v",
		"-ginkgo.show-node-events",
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
