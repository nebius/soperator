package login

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/values"
)

func TestGenerateSshdConfig_AuthorizedKeysCommandDependsOnSSSD(t *testing.T) {
	cluster := &values.SlurmCluster{
		NodeLogin: values.SlurmLogin{
			ContainerSshd: values.Container{
				NodeContainer: slurmv1.NodeContainer{Port: 22},
			},
		},
	}

	withoutSSSD := generateSshdConfig(cluster).Render()
	assert.NotContains(t, withoutSSSD, "AuthorizedKeysCommand /usr/bin/sss_ssh_authorizedkeys")

	cluster.NodeLogin.ContainerSSSD = &values.Container{
		NodeContainer: slurmv1.NodeContainer{Image: "sssd-image"},
	}

	withSSSD := generateSshdConfig(cluster).Render()
	assert.Contains(t, withSSSD, "AuthorizedKeysCommand /usr/bin/sss_ssh_authorizedkeys")
	assert.Contains(t, withSSSD, "AuthorizedKeysCommandUser root")
}

func TestGenerateUserIsolationConfig_Disabled(t *testing.T) {
	cluster := &values.SlurmCluster{}

	// Not configured at all.
	rendered := generateUserIsolationConfig(cluster).Render()
	assert.Contains(t, rendered, "SOPERATOR_USER_ISOLATION_ENABLED=false")
	assert.NotContains(t, rendered, "SOPERATOR_USER_ISOLATION_MEMORY_HIGH")
	assert.NotContains(t, rendered, "SOPERATOR_USER_ISOLATION_MEMORY_MAX")
	assert.NotContains(t, rendered, "SOPERATOR_USER_ISOLATION_CPU_WEIGHT")

	// Configured but explicitly disabled.
	cluster.NodeLogin.UserIsolation = &slurmv1.LoginUserIsolation{Enabled: false}
	rendered = generateUserIsolationConfig(cluster).Render()
	assert.Contains(t, rendered, "SOPERATOR_USER_ISOLATION_ENABLED=false")
}

func TestGenerateUserIsolationConfig_Enabled(t *testing.T) {
	memoryHigh := resource.MustParse("2Gi")
	memoryMax := resource.MustParse("3Gi")
	cluster := &values.SlurmCluster{
		NodeLogin: values.SlurmLogin{
			UserIsolation: &slurmv1.LoginUserIsolation{
				Enabled:    true,
				MemoryHigh: &memoryHigh,
				MemoryMax:  &memoryMax,
				CPUWeight:  250,
			},
		},
	}

	rendered := generateUserIsolationConfig(cluster).Render()
	assert.Contains(t, rendered, "SOPERATOR_USER_ISOLATION_ENABLED=true")
	assert.Contains(t, rendered, "SOPERATOR_USER_ISOLATION_MEMORY_HIGH=2147483648")
	assert.Contains(t, rendered, "SOPERATOR_USER_ISOLATION_MEMORY_MAX=3221225472")
	assert.Contains(t, rendered, "SOPERATOR_USER_ISOLATION_CPU_WEIGHT=250")
}

func TestGenerateUserIsolationConfig_DerivedMemoryDefaults(t *testing.T) {
	cluster := &values.SlurmCluster{
		NodeLogin: values.SlurmLogin{
			ContainerSshd: values.Container{
				NodeContainer: slurmv1.NodeContainer{
					Resources: corev1.ResourceList{
						corev1.ResourceMemory: resource.MustParse("10Gi"),
					},
				},
			},
			UserIsolation: &slurmv1.LoginUserIsolation{Enabled: true},
		},
	}

	rendered := generateUserIsolationConfig(cluster).Render()
	assert.Contains(t, rendered, "SOPERATOR_USER_ISOLATION_ENABLED=true")
	// Unset memory limits are derived from the sshd container memory limit (10Gi):
	// memory.high = 80%, memory.max = 90%.
	assert.Contains(t, rendered, "SOPERATOR_USER_ISOLATION_MEMORY_HIGH=8589934592")
	assert.Contains(t, rendered, "SOPERATOR_USER_ISOLATION_MEMORY_MAX=9663676416")
	// CPUWeight falls back to 100 when the CRD default was not applied.
	assert.Contains(t, rendered, "SOPERATOR_USER_ISOLATION_CPU_WEIGHT=100")
}

func TestGenerateUserIsolationConfig_EnabledWithoutContainerMemory(t *testing.T) {
	cluster := &values.SlurmCluster{
		NodeLogin: values.SlurmLogin{
			UserIsolation: &slurmv1.LoginUserIsolation{Enabled: true},
		},
	}

	rendered := generateUserIsolationConfig(cluster).Render()
	assert.Contains(t, rendered, "SOPERATOR_USER_ISOLATION_ENABLED=true")
	// No container memory limit to derive from: memory limits are omitted so the
	// PAM hook skips them instead of writing zero.
	assert.NotContains(t, rendered, "SOPERATOR_USER_ISOLATION_MEMORY_HIGH")
	assert.NotContains(t, rendered, "SOPERATOR_USER_ISOLATION_MEMORY_MAX")
	assert.Contains(t, rendered, "SOPERATOR_USER_ISOLATION_CPU_WEIGHT=100")
}
