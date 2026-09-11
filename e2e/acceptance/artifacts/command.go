package artifacts

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

func collectArgs(
	ctx context.Context,
	destination string,
	filename string,
	scope framework.ArgsScope,
	args ...string,
) error {
	if scope == nil {
		return fmt.Errorf("run %s: command scope is unavailable", strings.Join(args, " "))
	}
	output, commandErr := scope.Run(ctx, args...)
	writeErr := writeArtifact(destination, filename, output)
	if commandErr != nil && writeErr != nil {
		return errors.Join(
			fmt.Errorf("run %s: %w", strings.Join(args, " "), commandErr),
			fmt.Errorf("write command output: %w", writeErr),
		)
	}
	if commandErr != nil {
		return fmt.Errorf("run %s: %w", strings.Join(args, " "), commandErr)
	}
	return writeErr
}

func collectCommand(
	ctx context.Context,
	destination string,
	filename string,
	scope framework.CommandScope,
	command string,
) error {
	if scope == nil {
		return fmt.Errorf("run %s: command scope is unavailable", command)
	}
	output, commandErr := scope.Run(ctx, command)
	writeErr := writeArtifact(destination, filename, output)
	if commandErr != nil && writeErr != nil {
		return errors.Join(
			fmt.Errorf("run %s: %w", command, commandErr),
			fmt.Errorf("write command output: %w", writeErr),
		)
	}
	if commandErr != nil {
		return fmt.Errorf("run %s: %w", command, commandErr)
	}
	return writeErr
}

func writeArtifact(destination, filename, content string) error {
	artifactPath := filepath.Join(destination, filepath.Clean(filename))
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0o755); err != nil {
		return fmt.Errorf("create artifact parent directory: %w", err)
	}
	if err := os.WriteFile(artifactPath, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write artifact %q: %w", artifactPath, err)
	}
	return nil
}
