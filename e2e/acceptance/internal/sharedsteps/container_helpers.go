package sharedsteps

import (
	"context"
	"fmt"
	"strings"

	"github.com/nebius/soperator/e2e/acceptance/framework"
)

const (
	containerLifecycleCommand = "echo ready; sleep 3600"

	// Mirrored Ubuntu has /bin/bash required by the Slurm task prolog under Pyxis
	// and by Bash-based probes in Docker containers.
	ubuntuImageRef       = "soperator/ubuntu:noble"
	enrootLifecycleImage = "docker://cr.nebius.cloud#" + ubuntuImageRef
	ubuntuDockerImage    = "cr.nebius.cloud/" + ubuntuImageRef

	// Cluster-supported GPU diagnostic image (multi-GB); includes nvidia-smi for
	// container GPU visibility. Only the Enroot GPU smoke uses it, to cover a
	// realistic CUDA image. Other GPU container checks use the small Ubuntu image.
	// https://github.com/nebius/ml-containers/pull/121
	gpuSmokeEnrootImage = "docker://cr.nebius.cloud#ml-containers/training_diag:13.0.3-ubuntu24.04-20261021155223"
)

func assertJobStdoutReportsVisibleGPUs(ctx context.Context, exec framework.Exec, job framework.SbatchJob) error {
	stdout, err := readJobFile(ctx, exec, job.StdoutPath)
	if err != nil {
		return err
	}
	return assertGPUListing(stdout, fmt.Sprintf("job stdout %s", job.StdoutPath))
}

func assertGPUListing(output, source string) error {
	if strings.Contains(output, "GPU ") {
		return nil
	}
	return fmt.Errorf("expected nvidia-smi GPU listing in %s, got output:\n%s", source, strings.TrimSpace(output))
}
