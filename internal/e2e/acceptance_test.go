package e2e

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAcceptanceArgsForConfig(t *testing.T) {
	args := acceptanceArgsForConfig(Config{
		RunUnstableTests: true,
		SlurmClusterName: "custom",
	}, "dev-context")

	assert.Equal(t, []string{
		"--kubectl-context", "dev-context",
		"--slurm-cluster-name", "custom",
		"--run-unstable=true",
		"--report-dir", defaultAcceptanceReportDir,
	}, args)
}
