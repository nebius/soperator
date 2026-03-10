package worker_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"sigs.k8s.io/controller-runtime/pkg/client"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/worker"
	"nebius.ai/slurm-operator/internal/values"
)

func TestRenderNodeSetStatefulSet_SSSD(t *testing.T) {
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

	nodeSet := &values.SlurmNodeSet{
		Name: "worker-a",
		ParentalCluster: client.ObjectKey{
			Namespace: "test-ns",
			Name:      "test-cluster",
		},
		ContainerSlurmd: values.Container{
			NodeContainer: slurmv1.NodeContainer{
				Image: "slurmd-image",
				Port:  6818,
				Resources: corev1.ResourceList{
					corev1.ResourceCPU:              resource.MustParse("100m"),
					corev1.ResourceMemory:           resource.MustParse("1Gi"),
					corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
				},
			},
			Name: consts.ContainerNameSlurmd,
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
		ContainerSSSD:            &sssdContainer,
		SSSDConfSecretName:       "worker-sssd-secret",
		VolumeSpool:              corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		VolumeJail:               corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}},
		StatefulSet:              values.StatefulSet{Name: "worker-sts", Replicas: 1},
		ServiceUmbrella:          values.Service{Name: "worker-umbrella"},
		SupervisorDConfigMapName: "supervisord-config",
		SSHDConfigMapName:        "sshd-config",
		GPU:                      &slurmv1alpha1.GPUSpec{Enabled: false},
		CustomInitContainers:     []corev1.Container{},
	}

	result, err := worker.RenderNodeSetStatefulSet("test-cluster", nodeSet, &slurmv1.Secrets{}, consts.CGroupV2, false)
	assert.NoError(t, err)

	assert.Len(t, result.Spec.Template.Spec.InitContainers, 3)
	assert.Equal(t, consts.ContainerNameSSSD, result.Spec.Template.Spec.InitContainers[1].Name)

	var volumeNames []string
	for _, volume := range result.Spec.Template.Spec.Volumes {
		volumeNames = append(volumeNames, volume.Name)
	}
	assert.Contains(t, volumeNames, consts.VolumeNameSSSDConf)
	assert.Contains(t, volumeNames, consts.VolumeNameSSSDSocket)

	slurmd := result.Spec.Template.Spec.Containers[0]
	var mountNames []string
	for _, mount := range slurmd.VolumeMounts {
		mountNames = append(mountNames, mount.Name)
	}
	assert.Contains(t, mountNames, consts.VolumeNameSSSDConf)
	assert.Contains(t, mountNames, consts.VolumeNameSSSDSocket)
}
