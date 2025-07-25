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
			WaitForController: ptr.To(true),
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
			expectedInitCt: 2,
		},
		{
			name:           "CGROUP V1 GPU",
			worker:         createWorker(),
			secrets:        secret,
			clusterType:    consts.ClusterTypeGPU,
			expectedEnvVar: "",
			expectedInitCt: 3,
		},
		{
			name:           "CGROUP V2",
			worker:         createWorker(),
			secrets:        secret,
			clusterType:    consts.ClusterTypeCPU,
			expectedEnvVar: consts.CGroupV2Env,
			expectedInitCt: 2,
		},
		{
			name:           "CGROUP V2 GPU",
			worker:         createWorker(),
			secrets:        secret,
			clusterType:    consts.ClusterTypeGPU,
			expectedEnvVar: consts.CGroupV2Env,
			expectedInitCt: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := worker.RenderStatefulSet(
				testNamespace, testCluster, tt.clusterType, nodeFilter, tt.secrets, volumeSource, tt.worker, nil,
			)
			assert.NoError(t, err)

			assert.Equal(t, consts.ContainerNameSlurmd, result.Spec.Template.Spec.Containers[0].Name)
			assert.Equal(t, tt.expectedInitCt, len(result.Spec.Template.Spec.InitContainers))
			assert.Equal(t, consts.ContainerNameMunge, result.Spec.Template.Spec.InitContainers[0].Name)
			assert.Equal(t, consts.ContainerNameWaitForController, result.Spec.Template.Spec.InitContainers[1].Name)
			if tt.clusterType == consts.ClusterTypeGPU {
				assert.Equal(t, consts.ContainerNameToolkitValidation, result.Spec.Template.Spec.InitContainers[2].Name)
			}

			// Verify core expected volumes are present and no slurm-configs volume
			volumes := result.Spec.Template.Spec.Volumes
			var hasJail, hasMungeKey, hasMungeSocket, hasSlurmConfigs bool

			for _, volume := range volumes {
				switch volume.Name {
				case consts.VolumeNameJail:
					hasJail = true
				case consts.VolumeNameMungeKey:
					hasMungeKey = true
				case consts.VolumeNameMungeSocket:
					hasMungeSocket = true
				case consts.VolumeNameSlurmConfigs:
					hasSlurmConfigs = true
				}
			}

			assert.True(t, hasJail, "Worker StatefulSet should have jail volume")
			assert.True(t, hasMungeKey, "Worker StatefulSet should have munge-key volume")
			assert.True(t, hasMungeSocket, "Worker StatefulSet should have munge-socket volume")
			assert.False(t, hasSlurmConfigs, "Worker StatefulSet should NOT have slurm-configs volume")
		})
	}
}

func Test_RenderContainerWaitForController(t *testing.T) {
	container := &values.Container{
		NodeContainer: slurmv1.NodeContainer{
			Image:           "test-image",
			ImagePullPolicy: corev1.PullIfNotPresent,
		},
	}

	result := worker.RenderContainerWaitForController(container)

	assert.Equal(t, consts.ContainerNameWaitForController, result.Name)
	assert.Equal(t, container.Image, result.Image)
	assert.Equal(t, container.ImagePullPolicy, result.ImagePullPolicy)
	assert.Equal(t, []string{"/opt/bin/slurm/wait-for-controller.sh"}, result.Command)
	assert.Equal(t, 0, len(result.Env))
	assert.Equal(t, 2, len(result.VolumeMounts))

	// Verify exact volume mount values and no unexpected mounts
	expectedMounts := map[string]string{
		consts.VolumeNameJail:        consts.VolumeMountPathJail,
		consts.VolumeNameMungeSocket: consts.VolumeMountPathMungeSocket,
	}

	assert.Equal(t, len(expectedMounts), len(result.VolumeMounts))

	for _, mount := range result.VolumeMounts {
		expectedPath, exists := expectedMounts[mount.Name]
		assert.True(t, exists, "Unexpected volume mount: %s", mount.Name)
		assert.Equal(t, expectedPath, mount.MountPath, "Wrong mount path for volume %s", mount.Name)
	}
}
