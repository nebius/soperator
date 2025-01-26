package checkcontroller

import (
	"context"
	"os/exec"
)

type ScontrolRunner struct{}

func (s *ScontrolRunner) ShowNodes(ctx context.Context) ([]byte, error) {
	cmd := exec.CommandContext(ctx, "scontrol", "show", "nodes", "--json")
	return cmd.Output()
}
