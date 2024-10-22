package worker_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/worker"
	"nebius.ai/slurm-operator/internal/values"
)

func Test_RenderStatefulSet(t *testing.T) {
	testNamespace := "test-namespace"
	testCluster := "test-cluster"
	nodeFilter := []slurmv1.K8sNodeFilter{
		{
			Name: "cpu",
			NodeSelector: map[string]string{
				"test-node-selector": "test-node-selector-value",
			},
			Affinity: &corev1.Affinity{
				NodeAffinity: &corev1.NodeAffinity{
					RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
						NodeSelectorTerms: []corev1.NodeSelectorTerm{
							{
								MatchExpressions: []corev1.NodeSelectorRequirement{
									{
										Key:      "test-node-selector-key",
										Operator: corev1.NodeSelectorOpIn,
										Values:   []string{"test-node-selector-value"},
									},
								},
							},
						},
					},
				},
			},
		},
	}

	voluemSource := []slurmv1.VolumeSource{
		{
			Name: "test-volume-source",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{},
			},
		},
	}

	// Define a test worker
	workerCGroupV1 := &values.SlurmWorker{
		CgroupVersion: consts.CGroupV1,
		SlurmNode: slurmv1.SlurmNode{
			K8sNodeFilterName: "cpu",
		},
		VolumeSpool: slurmv1.NodeVolume{
			VolumeSourceName: ptr.To("test-volume-source"),
		},
		VolumeJail: slurmv1.NodeVolume{
			VolumeSourceName: ptr.To("test-volume-source"),
		},
	}

	result, err := worker.RenderStatefulSet(testNamespace, testCluster, consts.ClusterTypeGPU, nodeFilter, voluemSource, workerCGroupV1)
	assert.NoError(t, err)

	assert.Equal(t, consts.ContainerNameSlurmd, result.Spec.Template.Spec.Containers[0].Name)
	assert.Equal(t, consts.ContainerNameMunge, result.Spec.Template.Spec.Containers[1].Name)
	assert.Equal(t, consts.ContainerNameToolkitValidation, result.Spec.Template.Spec.InitContainers[0].Name)
	assert.True(t, len(result.Spec.Template.Spec.InitContainers) == 1)

	workerCGroupV2 := workerCGroupV1
	workerCGroupV2.CgroupVersion = consts.CGroupV2

	result, err = worker.RenderStatefulSet(testNamespace, testCluster, consts.ClusterTypeCPU, nodeFilter, voluemSource, workerCGroupV2)
	assert.NoError(t, err)
	assert.Equal(t, consts.CGroupV2Env, result.Spec.Template.Spec.Containers[0].Env[4].Name)
	assert.True(t, len(result.Spec.Template.Spec.InitContainers) == 0)
}
