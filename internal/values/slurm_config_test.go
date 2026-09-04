package values

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
)

func Test_buildSlurmConfigFrom(t *testing.T) {
	t.Run("keeps an explicit ResumeTimeout", func(t *testing.T) {
		result := buildSlurmConfigFrom(&slurmv1.SlurmConfig{
			ResumeTimeout: ptr.To[int32](3600),
		})

		require.NotNil(t, result.ResumeTimeout)
		assert.Equal(t, int32(3600), *result.ResumeTimeout)
	})

	t.Run("defaults an unset ResumeTimeout", func(t *testing.T) {
		// Rendering skips nil pointers, so leaving it unset would drop ResumeTimeout from
		// slurm.conf and hand ephemeral nodes Slurm's own 60 second default.
		result := buildSlurmConfigFrom(&slurmv1.SlurmConfig{})

		require.NotNil(t, result.ResumeTimeout)
		assert.Equal(t, int32(consts.SlurmDefaultResumeTimeout), *result.ResumeTimeout)
	})

	t.Run("does not mutate the spec it copies from", func(t *testing.T) {
		spec := &slurmv1.SlurmConfig{}

		buildSlurmConfigFrom(spec)

		assert.Nil(t, spec.ResumeTimeout)
	})

	t.Run("carries other fields through unchanged", func(t *testing.T) {
		result := buildSlurmConfigFrom(&slurmv1.SlurmConfig{
			SuspendTime:   ptr.To[int32](600),
			TopologyParam: "SwitchAsNodeRank",
		})

		require.NotNil(t, result.SuspendTime)
		assert.Equal(t, int32(600), *result.SuspendTime)
		assert.Equal(t, "SwitchAsNodeRank", result.TopologyParam)
	})
}
