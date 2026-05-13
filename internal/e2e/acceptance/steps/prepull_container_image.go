package steps

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/e2e/acceptance/framework"
)

const prepullActiveCheckName = "prepull-container-image"

var containerImageRegex = regexp.MustCompile(`--container-image=(\S+)`)

// PrepullContainerImagePyxis returns the Pyxis-format image declared in the
// prepull-container-image ActiveCheck's sbatch script
// (e.g. "cr.eu-north1.nebius.cloud#ml-containers/training_diag:<tag>").
func PrepullContainerImagePyxis(ctx context.Context, exec framework.Exec) (string, error) {
	var checks slurmv1alpha1.ActiveCheckList
	if err := kubectlJSON(ctx, exec, &checks, "get", "activechecks", "-A", "-o", "json"); err != nil {
		return "", fmt.Errorf("list ActiveChecks: %w", err)
	}
	for _, c := range checks.Items {
		if c.Name != prepullActiveCheckName {
			continue
		}
		script := ""
		if c.Spec.SlurmJobSpec.SbatchScript != nil {
			script = *c.Spec.SlurmJobSpec.SbatchScript
		}
		if script == "" {
			return "", fmt.Errorf("ActiveCheck %s has empty sbatchScript", prepullActiveCheckName)
		}
		m := containerImageRegex.FindStringSubmatch(script)
		if len(m) < 2 {
			return "", fmt.Errorf("ActiveCheck %s sbatchScript has no --container-image= argument", prepullActiveCheckName)
		}
		image := strings.TrimPrefix(strings.TrimSpace(m[1]), "docker://")
		if image == "" {
			return "", fmt.Errorf("ActiveCheck %s --container-image= is empty", prepullActiveCheckName)
		}
		return image, nil
	}
	return "", fmt.Errorf("ActiveCheck %s not found", prepullActiveCheckName)
}

// PrepullContainerImageDocker returns the same image in plain Docker URL form
// ("reg/repo:tag"), which is what `docker run` accepts.
func PrepullContainerImageDocker(ctx context.Context, exec framework.Exec) (string, error) {
	pyxis, err := PrepullContainerImagePyxis(ctx, exec)
	if err != nil {
		return "", err
	}
	return strings.Replace(pyxis, "#", "/", 1), nil
}

// PrepullContainerImageEnroot returns the image with the `docker://` Pyxis prefix
// expected by `srun --container-image=`.
func PrepullContainerImageEnroot(ctx context.Context, exec framework.Exec) (string, error) {
	pyxis, err := PrepullContainerImagePyxis(ctx, exec)
	if err != nil {
		return "", err
	}
	return "docker://" + pyxis, nil
}
