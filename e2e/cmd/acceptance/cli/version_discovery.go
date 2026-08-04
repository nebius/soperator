package cli

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"nebius.ai/soperator-e2e/acceptance/framework"
)

const (
	soperatorHelmReleaseNamespace  = "flux-system"
	soperatorHelmReleaseName       = "flux-system-soperator-fluxcd-soperator"
	versionDiscoveryCommandTimeout = time.Minute
)

// discoverFluxSoperatorVersion reads the Soperator chart revision from the
// standard Flux HelmRelease installed on Soperator clusters.
func discoverFluxSoperatorVersion(ctx context.Context, kubectlContext string) (string, error) {
	version, err := kubectlGetSoperatorHelmReleaseRevision(ctx, kubectlContext)
	if err != nil {
		return "", fmt.Errorf("get Soperator HelmRelease revision: %w", err)
	}

	return soperatorVersionFromHelmReleaseRevisions(version)
}

func kubectlGetSoperatorHelmReleaseRevision(ctx context.Context, kubectlContext string) (string, error) {
	cmdCtx, cancel := context.WithTimeout(ctx, versionDiscoveryCommandTimeout)
	defer cancel()

	args := appendKubectlContext(
		kubectlContext,
		"-n", soperatorHelmReleaseNamespace,
		"get", "helmrelease", soperatorHelmReleaseName,
		"-o", "jsonpath={.status.lastAppliedRevision}{\"\\n\"}{.status.lastAttemptedRevision}",
	)
	cmd := exec.CommandContext(cmdCtx, "kubectl", args...)

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		errOut := strings.TrimSpace(stderr.String())
		if errOut != "" {
			return stdout.String(), fmt.Errorf("kubectl %s: %w: %s", strings.Join(args, " "), err, errOut)
		}
		return stdout.String(), fmt.Errorf("kubectl %s: %w", strings.Join(args, " "), err)
	}

	return stdout.String(), nil
}

func appendKubectlContext(kubectlContext string, args ...string) []string {
	if strings.TrimSpace(kubectlContext) == "" {
		return append([]string(nil), args...)
	}

	out := make([]string, 0, len(args)+2)
	out = append(out, "--context", kubectlContext)
	out = append(out, args...)
	return out
}

func soperatorVersionFromHelmReleaseRevisions(raw string) (string, error) {
	lines := strings.Split(raw, "\n")
	var appliedRevision string
	if len(lines) > 0 {
		appliedRevision = strings.TrimSpace(lines[0])
	}
	if appliedRevision != "" {
		return validateSoperatorHelmReleaseVersion(appliedRevision)
	}

	var attemptedRevision string
	if len(lines) > 1 {
		attemptedRevision = strings.TrimSpace(lines[1])
	}
	if attemptedRevision != "" {
		return validateSoperatorHelmReleaseVersion(attemptedRevision)
	}

	return "", fmt.Errorf("Flux HelmRelease %s/%s has empty status.lastAppliedRevision and status.lastAttemptedRevision",
		soperatorHelmReleaseNamespace,
		soperatorHelmReleaseName,
	)
}

func validateSoperatorHelmReleaseVersion(version string) (string, error) {
	if version == "" {
		return "", fmt.Errorf("Flux HelmRelease %s/%s has empty revision",
			soperatorHelmReleaseNamespace,
			soperatorHelmReleaseName,
		)
	}
	if _, err := framework.NormalizeSoperatorVersion(version); err != nil {
		return "", fmt.Errorf("Flux HelmRelease %s/%s has unsupported deployed version %q: %w",
			soperatorHelmReleaseNamespace,
			soperatorHelmReleaseName,
			version,
			err,
		)
	}

	return version, nil
}
