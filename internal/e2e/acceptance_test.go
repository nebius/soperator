package e2e

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAcceptanceOptionsForConfig(t *testing.T) {
	opts := acceptanceOptionsForConfig(Config{
		RunUnstableTests: true,
		SlurmClusterName: "custom",
	}, "dev-context")

	assert.Equal(t, "dev-context", opts.KubectlContext)
	assert.Equal(t, "custom", opts.SlurmClusterName)
	assert.Equal(t, defaultAcceptanceReportDir, opts.ReportDir)
	assert.False(t, opts.ExcludeUnstable)
	assert.True(t, opts.ExcludeMissingWorkerKinds)
	assert.NotNil(t, opts.Features.FS)
	assert.NotEmpty(t, opts.Features.Paths)
	assert.Len(t, opts.StepRegistrars, 1)
	assert.Equal(t, "custom", opts.State.SlurmClusterName)
}
