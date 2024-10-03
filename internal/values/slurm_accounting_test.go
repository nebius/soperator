package values

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	"github.com/stretchr/testify/assert"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/naming"
)

func TestBuildAccountingFrom(t *testing.T) {
	defaultNameCluster := "test-cluster"
	image := "image-acc:latest"
	imageMunge := "test-muge:latest"
	memory := "512Mi"
	cpu := "100m"

	var port int32 = 8080

	accounting := &slurmv1.SlurmNodeAccounting{
		SlurmNode: slurmv1.SlurmNode{
			K8sNodeFilterName: "test-node-filter",
		},
		Enabled: true,
		ExternalDB: slurmv1.ExternalDB{
			Host: "test-host",
			Port: 5432,
		},
		Munge: slurmv1.NodeContainer{
			Image: imageMunge,
			Resources: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse(memory),
				corev1.ResourceCPU:    resource.MustParse(cpu),
			},
			Port: port,
		},
		Slurmdbd: slurmv1.NodeContainer{
			Image: image,
			Resources: corev1.ResourceList{
				corev1.ResourceMemory: resource.MustParse(memory),
				corev1.ResourceCPU:    resource.MustParse(cpu),
			},
		},
	}

	result := buildAccountingFrom(defaultNameCluster, accounting)

	assert.Equal(t, *accounting.SlurmNode.DeepCopy(), result.SlurmNode)
	assert.Equal(t, buildContainerFrom(accounting.Munge, consts.ContainerNameMunge).Name, result.ContainerMunge.Name)
	assert.Equal(t, buildContainerFrom(accounting.Slurmdbd, consts.ContainerNameAccounting).Image, result.ContainerAccounting.Image)
	assert.Equal(t, buildContainerFrom(accounting.Slurmdbd, consts.ContainerNameAccounting).Resources, result.ContainerAccounting.Resources)
	assert.Equal(t, buildContainerFrom(accounting.Slurmdbd, consts.ContainerNameMunge).Name, result.ContainerMunge.Name)
	assert.Equal(t, buildContainerFrom(accounting.Munge, consts.ContainerNameMunge).Image, result.ContainerMunge.Image)
	assert.Equal(t, buildContainerFrom(accounting.Munge, consts.ContainerNameMunge).Port, result.ContainerMunge.Port)
	assert.Equal(t, buildContainerFrom(accounting.Munge, consts.ContainerNameMunge).Resources, result.ContainerMunge.Resources)
	assert.Equal(t, buildServiceFrom(naming.BuildServiceName(consts.ComponentTypeAccounting, defaultNameCluster)), result.Service)
	assert.Equal(t, buildDeploymentFrom(naming.BuildDeploymentName(consts.ComponentTypeAccounting)), result.Deployment)
	assert.Equal(t, accounting.ExternalDB, result.ExternalDB)
	assert.Equal(t, accounting.Enabled, result.Enabled)
	assert.Equal(t, slurmv1.NodeVolume{VolumeSourceName: ptr.To(consts.VolumeNameJail)}, result.VolumeJail)
}
