package accounting_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
	accounting "nebius.ai/slurm-operator/internal/render/accounting"
	"nebius.ai/slurm-operator/internal/values"
)

func Test_RenderVolumeSlurmdbdConfigs(t *testing.T) {
	volume := accounting.RenderVolumeSlurmdbdConfigs(defaultNameCluster)
	assert.Equal(t, consts.VolumeNameSlurmdbdSecret, volume.Name)
	assert.Equal(t, naming.BuildSecretSlurmdbdConfigsName(defaultNameCluster), volume.VolumeSource.Secret.SecretName)
}

func Test_RenderVolumeMountSlurmdbdConfigs(t *testing.T) {
	volumeMount := accounting.RenderVolumeMountSlurmdbdConfigs()
	assert.Equal(t, consts.VolumeNameSlurmdbdSecret, volumeMount.Name)
	assert.Equal(t, consts.VolumeMountPathSlurmdbdSecret, volumeMount.MountPath)
	assert.True(t, volumeMount.ReadOnly)
}

func Test_RenderVolumeSlurmdbd(t *testing.T) {
	sizeGi := resource.MustParse("1Gi")
	testAcc := *acc
	testAcc.ContainerAccounting = values.Container{
		NodeContainer: slurmv1.NodeContainer{
			Resources: corev1.ResourceList{
				corev1.ResourceStorage: sizeGi,
			},
		},
	}
	volume := accounting.RenderVolumeSlurmdbdSpool(defaultNameCluster, &testAcc)
	assert.Equal(t, consts.VolumeNameSpool, volume.Name)
	assert.Equal(t, corev1.StorageMediumDefault, volume.VolumeSource.EmptyDir.Medium)
	assert.Equal(t, &sizeGi, volume.VolumeSource.EmptyDir.SizeLimit)
	volumeEmpty := accounting.RenderVolumeSlurmdbdSpool(defaultNameCluster, acc)
	assert.Equal(t, &resource.Quantity{Format: "BinarySI"}, volumeEmpty.VolumeSource.EmptyDir.SizeLimit)
}
