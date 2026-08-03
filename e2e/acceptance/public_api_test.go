package acceptance_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"nebius.ai/slurm-operator/e2e/acceptance"
	"nebius.ai/slurm-operator/e2e/acceptance/framework"
)

func TestPublicRunnerAPIIsImportable(t *testing.T) {
	features := acceptance.SharedFeatureSource()

	require.NotNil(t, features.FS)
	assert.Contains(t, features.Paths, "features/internal_ssh.feature")

	runner, err := acceptance.NewRunner(acceptance.Options{
		KubectlContext:            "dev-context",
		TargetSoperatorVersion:    "4.2.0",
		Features:                  features,
		Tags:                      "~@custom_only",
		ExcludeUnstable:           true,
		ExcludeMissingWorkerKinds: true,
		State:                     &framework.ClusterState{},
		StepRegistrars:            []acceptance.StepRegistrar{acceptance.SharedStepRegistrar()},
	})
	require.NoError(t, err)
	assert.NotNil(t, runner)
}
