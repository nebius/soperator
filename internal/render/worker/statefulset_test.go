package worker_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/worker"
	"nebius.ai/slurm-operator/internal/values"
)

func Test_RenderStatefulSet(t *testing.T) {
	testNamespace := "test-namespace"
	testCluster := "test-cluster"
	testTopologyConfig := "test-topology-config"
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

	volumeSource := []slurmv1.VolumeSource{
		{
			Name: "test-volume-source",
			VolumeSource: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{},
			},
		},
	}

	createWorker := func() *values.SlurmWorker {
		return &values.SlurmWorker{
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
			ContainerSlurmd: values.Container{
				NodeContainer: slurmv1.NodeContainer{
					Image:           "test-image",
					ImagePullPolicy: corev1.PullIfNotPresent,
					Port:            8080,
					Resources: corev1.ResourceList{
						corev1.ResourceMemory:           resource.MustParse("1Gi"),
						corev1.ResourceCPU:              resource.MustParse("100m"),
						corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
					},
				},
			},
		}
	}

	secret := &slurmv1.Secrets{
		SshdKeysName: "test-sshd-keys",
	}

	tests := []struct {
		name           string
		worker         *values.SlurmWorker
		secrets        *slurmv1.Secrets
		clusterType    consts.ClusterType
		expectedEnvVar string
		expectedInitCt int
	}{
		{
			name:           "CGROUP V1 GPU",
			worker:         createWorker(),
			secrets:        secret,
			clusterType:    consts.ClusterTypeCPU,
			expectedEnvVar: "",
			expectedInitCt: 1,
		},
		{
			name:           "CGROUP V1 GPU",
			worker:         createWorker(),
			secrets:        secret,
			clusterType:    consts.ClusterTypeGPU,
			expectedEnvVar: "",
			expectedInitCt: 2,
		},
		{
			name:           "CGROUP V2",
			worker:         createWorker(),
			secrets:        secret,
			clusterType:    consts.ClusterTypeCPU,
			expectedEnvVar: consts.CGroupV2Env,
			expectedInitCt: 1,
		},
		{
			name:           "CGROUP V2 GPU",
			worker:         createWorker(),
			secrets:        secret,
			clusterType:    consts.ClusterTypeGPU,
			expectedEnvVar: consts.CGroupV2Env,
			expectedInitCt: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := worker.RenderStatefulSet(testNamespace, testCluster, tt.clusterType, nodeFilter, tt.secrets, volumeSource, tt.worker, testTopologyConfig, nil)
			assert.NoError(t, err)

			assert.Equal(t, consts.ContainerNameSlurmd, result.Spec.Template.Spec.Containers[0].Name)
			if tt.clusterType == consts.ClusterTypeGPU {
				assert.Equal(t, consts.ContainerNameToolkitValidation, result.Spec.Template.Spec.InitContainers[1].Name)
			}
			assert.Equal(t, tt.expectedInitCt, len(result.Spec.Template.Spec.InitContainers))
			assert.Equal(t, consts.ContainerNameMunge, result.Spec.Template.Spec.InitContainers[0].Name)
		})
	}
}
