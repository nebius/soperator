package values

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
)

func TestBuildSlurmLoginFrom_SSSD(t *testing.T) {
	t.Run("uses default secret name when ref is empty", func(t *testing.T) {
		login := &slurmv1.SlurmNodeLogin{
			SlurmNode: slurmv1.SlurmNode{},
			Sshd:      slurmv1.NodeContainer{Image: "sshd-image"},
			Munge:     slurmv1.NodeContainer{Image: "munge-image"},
			SidecarSSSD: slurmv1.SidecarSSSD{
				Sssd: &slurmv1.NodeContainer{Image: "sssd-image"},
			},
			Volumes: slurmv1.SlurmNodeLoginVolumes{Jail: slurmv1.NodeVolume{}},
		}

		result := buildSlurmLoginFrom("test-cluster", "", nil, login, false)

		if assert.NotNil(t, result.ContainerSSSD) {
			assert.Equal(t, "sssd-image", result.ContainerSSSD.Image)
			assert.Equal(t, consts.ContainerNameSSSD, result.ContainerSSSD.Name)
		}
		assert.True(t, result.IsSSSDSecretDefault)
		assert.Equal(t, naming.BuildSecretSSSDConfName("test-cluster"), result.SSSDConfSecretName)
	})

	t.Run("uses referenced secret name when ref is set", func(t *testing.T) {
		login := &slurmv1.SlurmNodeLogin{
			Sshd:  slurmv1.NodeContainer{Image: "sshd-image"},
			Munge: slurmv1.NodeContainer{Image: "munge-image"},
			SidecarSSSD: slurmv1.SidecarSSSD{
				Sssd:                  &slurmv1.NodeContainer{Image: "sssd-image"},
				SSSDConfSecretRefName: "custom-sssd-secret",
			},
			Volumes: slurmv1.SlurmNodeLoginVolumes{Jail: slurmv1.NodeVolume{}},
		}

		result := buildSlurmLoginFrom("test-cluster", "", nil, login, false)

		assert.False(t, result.IsSSSDSecretDefault)
		assert.Equal(t, "custom-sssd-secret", result.SSSDConfSecretName)
	})

	t.Run("keeps sssd disabled when container is not configured", func(t *testing.T) {
		login := &slurmv1.SlurmNodeLogin{
			Sshd:    slurmv1.NodeContainer{Image: "sshd-image"},
			Munge:   slurmv1.NodeContainer{Image: "munge-image"},
			Volumes: slurmv1.SlurmNodeLoginVolumes{Jail: slurmv1.NodeVolume{}},
		}

		result := buildSlurmLoginFrom("test-cluster", "", nil, login, false)

		assert.Nil(t, result.ContainerSSSD)
		assert.Equal(t, naming.BuildSecretSSSDConfName("test-cluster"), result.SSSDConfSecretName)
		assert.True(t, result.IsSSSDSecretDefault)
	})
}

func TestBuildSlurmLoginFrom_PreservesSSSHDConfigDefault(t *testing.T) {
	login := &slurmv1.SlurmNodeLogin{
		Sshd:    slurmv1.NodeContainer{Image: "sshd-image"},
		Munge:   slurmv1.NodeContainer{Image: "munge-image"},
		Volumes: slurmv1.SlurmNodeLoginVolumes{Jail: slurmv1.NodeVolume{}},
	}

	result := buildSlurmLoginFrom("test-cluster", "", nil, login, false)

	assert.Equal(t, naming.BuildConfigMapSSHDConfigsNameLogin("test-cluster"), result.SSHDConfigMapName)
	assert.True(t, result.IsSSHDConfigMapDefault)
	assert.Equal(t, corev1.ServiceTypeClusterIP, result.HeadlessService.Type)
}
