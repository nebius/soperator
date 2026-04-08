package values

import (
	"testing"

	"github.com/stretchr/testify/assert"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
)

func TestBuildSlurmControllerFrom_SSSD(t *testing.T) {
	t.Run("uses default secret name when ref is empty", func(t *testing.T) {
		controller := &slurmv1.SlurmNodeController{
			Slurmctld: slurmv1.NodeContainer{Image: "slurmctld-image"},
			Munge:     slurmv1.NodeContainer{Image: "munge-image"},
			SidecarSSSD: slurmv1.SidecarSSSD{
				Sssd: &slurmv1.NodeContainer{Image: "sssd-image"},
			},
			Volumes: slurmv1.SlurmNodeControllerVolumes{Spool: slurmv1.NodeVolume{}, Jail: slurmv1.NodeVolume{}},
		}

		result := buildSlurmControllerFrom("test-cluster", "", nil, controller)

		if assert.NotNil(t, result.ContainerSSSD) {
			assert.Equal(t, "sssd-image", result.ContainerSSSD.Image)
			assert.Equal(t, consts.ContainerNameSSSD, result.ContainerSSSD.Name)
		}
		assert.True(t, result.IsSSSDSecretDefault)
		assert.Equal(t, naming.BuildSecretSSSDConfName("test-cluster"), result.SSSDConfSecretName)
	})

	t.Run("uses referenced secret name when ref is set", func(t *testing.T) {
		controller := &slurmv1.SlurmNodeController{
			Slurmctld: slurmv1.NodeContainer{Image: "slurmctld-image"},
			Munge:     slurmv1.NodeContainer{Image: "munge-image"},
			SidecarSSSD: slurmv1.SidecarSSSD{
				Sssd:                  &slurmv1.NodeContainer{Image: "sssd-image"},
				SSSDConfSecretRefName: "custom-controller-sssd-secret",
			},
			Volumes: slurmv1.SlurmNodeControllerVolumes{Spool: slurmv1.NodeVolume{}, Jail: slurmv1.NodeVolume{}},
		}

		result := buildSlurmControllerFrom("test-cluster", "", nil, controller)

		assert.False(t, result.IsSSSDSecretDefault)
		assert.Equal(t, "custom-controller-sssd-secret", result.SSSDConfSecretName)
	})

	t.Run("keeps sssd disabled when container is not configured", func(t *testing.T) {
		controller := &slurmv1.SlurmNodeController{
			Slurmctld: slurmv1.NodeContainer{Image: "slurmctld-image"},
			Munge:     slurmv1.NodeContainer{Image: "munge-image"},
			Volumes:   slurmv1.SlurmNodeControllerVolumes{Spool: slurmv1.NodeVolume{}, Jail: slurmv1.NodeVolume{}},
		}

		result := buildSlurmControllerFrom("test-cluster", "", nil, controller)

		assert.Nil(t, result.ContainerSSSD)
		assert.Equal(t, naming.BuildSecretSSSDConfName("test-cluster"), result.SSSDConfSecretName)
		assert.True(t, result.IsSSSDSecretDefault)
	})
}
