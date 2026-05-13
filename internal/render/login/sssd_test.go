package login

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/values"
)

func TestRenderStatefulSet_SSSD(t *testing.T) {
	sssdContainer := values.Container{
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

	login := &values.SlurmLogin{
		SlurmNode: slurmv1.SlurmNode{
			K8sNodeFilterName: "test-filter",
		},
		ContainerSshd: values.Container{
			NodeContainer: slurmv1.NodeContainer{
				Image: "sshd-image",
				Port:  22,
				Resources: corev1.ResourceList{
					corev1.ResourceCPU:              resource.MustParse("100m"),
					corev1.ResourceMemory:           resource.MustParse("1Gi"),
					corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
				},
			},
			Name: consts.ContainerNameSshd,
		},
		ContainerMunge: values.Container{
			NodeContainer: slurmv1.NodeContainer{
				Image: "munge-image",
				Resources: corev1.ResourceList{
					corev1.ResourceCPU:              resource.MustParse("100m"),
					corev1.ResourceMemory:           resource.MustParse("128Mi"),
					corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
				},
			},
			Name: consts.ContainerNameMunge,
		},
		ContainerSSSD: &sssdContainer,
		VolumeJail: slurmv1.NodeVolume{
			VolumeSourceName: &[]string{"test-volume"}[0],
		},
		StatefulSet:          values.StatefulSet{Name: "login", Replicas: 1},
		HeadlessService:      values.Service{Name: "login-headless"},
		SSHDConfigMapName:    "sshd-config",
		SSSDConfSecretName:   "sssd-secret",
		CustomInitContainers: []corev1.Container{},
	}

	result, err := RenderStatefulSet(
		"test-ns",
		"test-cluster",
		consts.ClusterTypeGPU,
		[]slurmv1.K8sNodeFilter{{Name: "test-filter"}},
		&slurmv1.Secrets{},
		[]slurmv1.VolumeSource{{Name: "test-volume", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}},
		login,
	)
	assert.NoError(t, err)

	assert.Len(t, result.Spec.Template.Spec.InitContainers, 2)
	assert.Equal(t, consts.ContainerNameSSSD, result.Spec.Template.Spec.InitContainers[1].Name)

	var volumeNames []string
	for _, volume := range result.Spec.Template.Spec.Volumes {
		volumeNames = append(volumeNames, volume.Name)
	}
	assert.Contains(t, volumeNames, consts.VolumeNameSSSDConf)
	assert.Contains(t, volumeNames, consts.VolumeNameSSSDSocket)

	sshd := result.Spec.Template.Spec.Containers[0]
	var mountNames []string
	for _, mount := range sshd.VolumeMounts {
		mountNames = append(mountNames, mount.Name)
	}
	assert.Contains(t, mountNames, consts.VolumeNameSSSDConf)
	assert.Contains(t, mountNames, consts.VolumeNameSSSDSocket)
}
