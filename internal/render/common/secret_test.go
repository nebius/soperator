package common

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/values"
)

func TestRenderSecretDefaultSSSDConf(t *testing.T) {
	secret := RenderSecretDefaultSSSDConf("test-ns", "test-cluster", "test-sssd", consts.ComponentTypeLogin)

	assert.Equal(t, "test-sssd", secret.Name)
	assert.Equal(t, "test-ns", secret.Namespace)
	assert.Contains(t, secret.Data, consts.SssdConfig)

	content := string(secret.Data[consts.SssdConfig])
	assert.Contains(t, content, "services = nss, pam")
	assert.Contains(t, content, "enable_files_domain = true")
	assert.NotContains(t, content, "domains = local")
	assert.NotContains(t, content, "id_provider = local")
}

func TestRenderContainerSSSD_DefaultCommandCreatesPrivatePipeDir(t *testing.T) {
	container := &values.Container{
		NodeContainer: slurmv1.NodeContainer{
			Image: "sssd-image",
			Resources: corev1.ResourceList{
				corev1.ResourceCPU:              resource.MustParse("100m"),
				corev1.ResourceMemory:           resource.MustParse("128Mi"),
				corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
			},
		},
		Name: consts.ContainerNameSSSD,
	}

	rendered := RenderContainerSSSD(container)

	assert.Equal(t, []string{"/bin/sh", "-c"}, rendered.Command)
	assert.Contains(t, rendered.Args[0], "mkdir -p /var/lib/sss/pipes/private")
	assert.Contains(t, rendered.Args[0], "chmod 700 /var/lib/sss/pipes/private")
	assert.Contains(t, rendered.Args[0], "exec /usr/sbin/sssd --interactive -d 0 --logger=stderr")
}
