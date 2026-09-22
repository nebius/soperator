package values

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
)

func TestBuildPAMSlurmAdoptFrom(t *testing.T) {
	t.Run("defaults to enabled", func(t *testing.T) {
		result := buildPAMSlurmAdoptFrom(nil)

		assert.True(t, result.Enabled)
		assert.Empty(t, result.ExemptUsers)
		assert.Empty(t, result.ExemptGroups)
	})

	t.Run("preserves configuration", func(t *testing.T) {
		config := &slurmv1.PAMSlurmAdopt{
			Enabled:      ptr.To(true),
			ExemptUsers:  []string{"service-user"},
			ExemptGroups: []string{"cluster-admins"},
		}

		result := buildPAMSlurmAdoptFrom(config)

		assert.True(t, result.Enabled)
		assert.Equal(t, config.ExemptUsers, result.ExemptUsers)
		assert.Equal(t, config.ExemptGroups, result.ExemptGroups)

		config.ExemptUsers[0] = "changed"
		config.ExemptGroups[0] = "changed"
		assert.Equal(t, []string{"service-user"}, result.ExemptUsers)
		assert.Equal(t, []string{"cluster-admins"}, result.ExemptGroups)
	})

	t.Run("can be disabled", func(t *testing.T) {
		result := buildPAMSlurmAdoptFrom(&slurmv1.PAMSlurmAdopt{Enabled: ptr.To(false)})

		assert.False(t, result.Enabled)
	})
}
