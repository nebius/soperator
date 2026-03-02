package worker_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"
	"sigs.k8s.io/controller-runtime/pkg/client"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/render/worker"
	"nebius.ai/slurm-operator/internal/values"
)

func Test_RenderContainerWorkerInit(t *testing.T) {
	container := &values.Container{
		NodeContainer: slurmv1.NodeContainer{
			Image:           "test-image",
			ImagePullPolicy: corev1.PullIfNotPresent,
		},
	}

	t.Run("with topology enabled", func(t *testing.T) {
		result := worker.RenderContainerWorkerInit("test-cluster", container, true, true, 300)

		assert.Equal(t, consts.ContainerNameWorkerInit, result.Name)
		assert.Equal(t, container.Image, result.Image)
		assert.Equal(t, container.ImagePullPolicy, result.ImagePullPolicy)
		assert.Equal(t, []string{"python3", "/opt/bin/slurm/worker_init.py", "wait-controller", "wait-topology"}, result.Command)
		assert.Equal(t, 10, len(result.Env)) // 6 base + 1 NODESET_GPU_ENABLED + 3 topology
		assert.Equal(t, 3, len(result.VolumeMounts))

		expectedMounts := map[string]string{
			consts.VolumeNameJail:               consts.VolumeMountPathJail,
			consts.VolumeNameMungeSocket:        consts.VolumeMountPathMungeSocket,
			consts.VolumeNameTopologyNodeLabels: consts.VolumeMountPathTopologyNodeLabels,
		}
		assert.Equal(t, len(expectedMounts), len(result.VolumeMounts))
		for _, mount := range result.VolumeMounts {
			expectedPath, exists := expectedMounts[mount.Name]
			assert.True(t, exists, "Unexpected volume mount: %s", mount.Name)
			assert.Equal(t, expectedPath, mount.MountPath, "Wrong mount path for volume %s", mount.Name)
		}
	})

	t.Run("without topology", func(t *testing.T) {
		result := worker.RenderContainerWorkerInit("test-cluster", container, false, false, 0)

		assert.Equal(t, consts.ContainerNameWorkerInit, result.Name)
		assert.Equal(t, container.Image, result.Image)
		assert.Equal(t, container.ImagePullPolicy, result.ImagePullPolicy)
		assert.Equal(t, []string{"python3", "/opt/bin/slurm/worker_init.py", "wait-controller"}, result.Command,
			"wait-topology should not be present when topology is disabled")
		assert.Equal(t, 6, len(result.Env), "only controller-related env vars expected")
		assert.Equal(t, 2, len(result.VolumeMounts), "topology volume mount should not be present")

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

		// Verify no topology env vars
		for _, envVar := range result.Env {
			assert.NotContains(t, []string{"TOPOLOGY_CONFIGMAP_PATH", "TOPOLOGY_WAIT_TIMEOUT", "TOPOLOGY_POLL_INTERVAL"}, envVar.Name,
				"topology env var %s should not be present when topology is disabled", envVar.Name)
		}
	})
}

func Test_RenderContainerWorkerInit_K8SServiceName(t *testing.T) {
	container := &values.Container{
		NodeContainer: slurmv1.NodeContainer{
			Image:           "test-image",
			ImagePullPolicy: corev1.PullIfNotPresent,
		},
	}

	findEnv := func(envs []corev1.EnvVar, name string) (corev1.EnvVar, bool) {
		for _, e := range envs {
			if e.Name == name {
				return e, true
			}
		}
		return corev1.EnvVar{}, false
	}

	tests := []struct {
		name            string
		clusterName     string
		gpuEnabled      bool
		expectedService string
	}{
		{
			name:            "uses nodeset service name with gpu enabled",
			clusterName:     "my-cluster",
			gpuEnabled:      true,
			expectedService: "my-cluster-nodeset-svc",
		},
		{
			name:            "uses nodeset service name without gpu",
			clusterName:     "my-cluster",
			gpuEnabled:      false,
			expectedService: "my-cluster-nodeset-svc",
		},
		{
			name:            "different cluster name with gpu enabled",
			clusterName:     "prod",
			gpuEnabled:      true,
			expectedService: "prod-nodeset-svc",
		},
		{
			name:            "different cluster name without gpu",
			clusterName:     "prod",
			gpuEnabled:      false,
			expectedService: "prod-nodeset-svc",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := worker.RenderContainerWorkerInit(tt.clusterName, container, false, tt.gpuEnabled, 0)

			env, found := findEnv(result.Env, "K8S_SERVICE_NAME")
			assert.True(t, found, "K8S_SERVICE_NAME env var must be present")
			assert.Equal(t, tt.expectedService, env.Value,
				"K8S_SERVICE_NAME should be %q for gpuEnabled=%v, got %q",
				tt.expectedService, tt.gpuEnabled, env.Value)
		})
	}
}

func TestRenderNodeSetStatefulSet_TopologyPlugin(t *testing.T) {
	createNodeSet := func(ephemeralNodes *bool, waitTimeout int32) *values.SlurmNodeSet {
		return &values.SlurmNodeSet{
			Name: "test-nodeset",
			ParentalCluster: client.ObjectKey{
				Namespace: "test-namespace",
				Name:      "test-cluster",
			},
			ContainerSlurmd: values.Container{
				NodeContainer: slurmv1.NodeContainer{
					Image:           "test-image",
					ImagePullPolicy: corev1.PullIfNotPresent,
					Resources: corev1.ResourceList{
						corev1.ResourceMemory:           resource.MustParse("1Gi"),
						corev1.ResourceCPU:              resource.MustParse("100m"),
						corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
					},
				},
			},
			ContainerMunge: values.Container{
				NodeContainer: slurmv1.NodeContainer{
					Image: "munge-image",
				},
			},
			VolumeSpool: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{Path: "/tmp/spool"},
			},
			VolumeJail: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{Path: "/tmp/jail"},
			},
			StatefulSet: values.StatefulSet{
				Replicas: 1,
			},
			SupervisorDConfigMapName:     "supervisord-config",
			SSHDConfigMapName:            "sshd-config",
			GPU:                          &slurmv1alpha1.GPUSpec{Enabled: false},
			EphemeralNodes:               ephemeralNodes,
			EphemeralTopologyWaitTimeout: waitTimeout,
		}
	}

	tests := []struct {
		name                       string
		ephemeralNodes             *bool
		waitTimeout                int32
		topologyPluginEnabled      bool
		expectedInitContainerCount int
		expectTopologyVolumes      bool
		expectWaitForTopology      bool
	}{
		{
			name:                       "topology plugin enabled with timeout",
			ephemeralNodes:             nil,
			waitTimeout:                300,
			topologyPluginEnabled:      true,
			expectedInitContainerCount: 2, // munge + worker-init
			expectTopologyVolumes:      true,
			expectWaitForTopology:      true,
		},
		{
			name:                       "topology plugin enabled with zero timeout uses default",
			ephemeralNodes:             ptr.To(true),
			waitTimeout:                0,
			topologyPluginEnabled:      true,
			expectedInitContainerCount: 2, // munge + worker-init
			expectTopologyVolumes:      true,
			expectWaitForTopology:      true, // default timeout is applied
		},
		{
			name:                       "topology plugin disabled",
			ephemeralNodes:             ptr.To(false),
			waitTimeout:                300,
			topologyPluginEnabled:      false,
			expectedInitContainerCount: 2, // munge + worker-init
			expectTopologyVolumes:      false,
			expectWaitForTopology:      false,
		},
		{
			name:                       "topology plugin disabled with ephemeral nodes",
			ephemeralNodes:             ptr.To(true),
			waitTimeout:                300,
			topologyPluginEnabled:      false,
			expectedInitContainerCount: 2,     // munge + worker-init
			expectTopologyVolumes:      false, // no volume without topology plugin
			expectWaitForTopology:      false, // no wait-topology without topology plugin
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodeSet := createNodeSet(tt.ephemeralNodes, tt.waitTimeout)

			result, err := worker.RenderNodeSetStatefulSet(
				"test-cluster",
				nodeSet,
				&slurmv1.Secrets{},
				consts.CGroupV2,
				tt.topologyPluginEnabled,
			)
			assert.NoError(t, err)

			// Verify init container count
			assert.Len(t, result.Spec.Template.Spec.InitContainers, tt.expectedInitContainerCount,
				"expected %d init containers", tt.expectedInitContainerCount)

			// Verify worker-init container has topology command when topology plugin is enabled
			var hasWaitForTopology bool
			for _, container := range result.Spec.Template.Spec.InitContainers {
				if container.Name == consts.ContainerNameWorkerInit {
					for _, arg := range container.Command {
						if arg == "wait-topology" {
							hasWaitForTopology = true
							break
						}
					}
					break
				}
			}
			assert.Equal(t, tt.expectWaitForTopology, hasWaitForTopology,
				"wait-topology command presence mismatch")

			// Verify topology-related volumes
			var hasTopologyNodeLabelsVolume bool
			for _, volume := range result.Spec.Template.Spec.Volumes {
				if volume.Name == consts.VolumeNameTopologyNodeLabels {
					hasTopologyNodeLabelsVolume = true
					if tt.expectTopologyVolumes {
						assert.NotNil(t, volume.ConfigMap, "topology-node-labels volume should be ConfigMap")
						assert.Equal(t, consts.ConfigMapNameTopologyNodeLabels, volume.ConfigMap.Name)
						assert.NotNil(t, volume.ConfigMap.Optional)
						assert.True(t, *volume.ConfigMap.Optional, "topology-node-labels ConfigMap should be optional")
					}
				}
			}
			assert.Equal(t, tt.expectTopologyVolumes, hasTopologyNodeLabelsVolume,
				"topology-node-labels volume presence mismatch")
		})
	}
}

func TestRenderNodeSetStatefulSet_EphemeralNodesReserveOrdinals(t *testing.T) {
	createNodeSetWithActiveNodes := func(ephemeralNodes bool, activeNodes []int32) *values.SlurmNodeSet {
		return &values.SlurmNodeSet{
			Name: "test-nodeset",
			ParentalCluster: client.ObjectKey{
				Namespace: "test-namespace",
				Name:      "test-cluster",
			},
			ContainerSlurmd: values.Container{
				NodeContainer: slurmv1.NodeContainer{
					Image:           "test-image",
					ImagePullPolicy: corev1.PullIfNotPresent,
					Resources: corev1.ResourceList{
						corev1.ResourceMemory:           resource.MustParse("1Gi"),
						corev1.ResourceCPU:              resource.MustParse("100m"),
						corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
					},
				},
			},
			ContainerMunge: values.Container{
				NodeContainer: slurmv1.NodeContainer{
					Image: "munge-image",
				},
			},
			VolumeSpool: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{Path: "/tmp/spool"},
			},
			VolumeJail: corev1.VolumeSource{
				HostPath: &corev1.HostPathVolumeSource{Path: "/tmp/jail"},
			},
			StatefulSet: values.StatefulSet{
				Name:     "test-sts",
				Replicas: 10, // Original replicas, should be overridden for ephemeral
			},
			ServiceUmbrella:          values.Service{Name: "test-umbrella"},
			SupervisorDConfigMapName: "supervisord-config",
			SSHDConfigMapName:        "sshd-config",
			GPU:                      &slurmv1alpha1.GPUSpec{Enabled: false},
			EphemeralNodes:           &ephemeralNodes,
			ActiveNodes:              activeNodes,
		}
	}

	tests := []struct {
		name                    string
		ephemeralNodes          bool
		activeNodes             []int32
		expectedReplicas        int32
		expectedReserveOrdinals []int32 // ordinal values
	}{
		{
			name:                    "ephemeral disabled uses original replicas",
			ephemeralNodes:          false,
			activeNodes:             []int32{0, 1, 2},
			expectedReplicas:        10, // Original replicas
			expectedReserveOrdinals: nil,
		},
		{
			name:                    "ephemeral enabled with empty activeNodes creates zero replicas",
			ephemeralNodes:          true,
			activeNodes:             []int32{},
			expectedReplicas:        0,
			expectedReserveOrdinals: nil,
		},
		{
			name:                    "ephemeral enabled with consecutive nodes",
			ephemeralNodes:          true,
			activeNodes:             []int32{0, 1, 2},
			expectedReplicas:        3,
			expectedReserveOrdinals: nil, // No gaps, no reserved ordinals
		},
		{
			name:                    "ephemeral enabled with gaps in ordinals",
			ephemeralNodes:          true,
			activeNodes:             []int32{0, 3, 5, 7, 12},
			expectedReplicas:        5,
			expectedReserveOrdinals: []int32{1, 2, 4, 6, 8, 9, 10, 11},
		},
		{
			name:                    "ephemeral enabled with single high ordinal",
			ephemeralNodes:          true,
			activeNodes:             []int32{5},
			expectedReplicas:        1,
			expectedReserveOrdinals: []int32{0, 1, 2, 3, 4},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodeSet := createNodeSetWithActiveNodes(tt.ephemeralNodes, tt.activeNodes)

			result, err := worker.RenderNodeSetStatefulSet("test-cluster", nodeSet, &slurmv1.Secrets{}, consts.CGroupV2, false)
			assert.NoError(t, err)

			// Verify replicas
			assert.NotNil(t, result.Spec.Replicas, "replicas should not be nil")
			assert.Equal(t, tt.expectedReplicas, *result.Spec.Replicas,
				"expected replicas to be %d, got %d", tt.expectedReplicas, *result.Spec.Replicas)

			// Verify reserveOrdinals
			if tt.expectedReserveOrdinals == nil {
				assert.Empty(t, result.Spec.ReserveOrdinals,
					"expected no reserveOrdinals, but got %v", result.Spec.ReserveOrdinals)
			} else {
				assert.Len(t, result.Spec.ReserveOrdinals, len(tt.expectedReserveOrdinals),
					"expected %d reserveOrdinals, got %d",
					len(tt.expectedReserveOrdinals), len(result.Spec.ReserveOrdinals))

				// Verify each reserved ordinal matches expected value
				for i, expected := range tt.expectedReserveOrdinals {
					actual := result.Spec.ReserveOrdinals[i].IntValue()
					assert.Equal(t, int(expected), actual,
						"reserveOrdinals[%d] mismatch: expected %d, got %d", i, expected, actual)
				}

				// Build sets for comprehensive verification
				activeSet := make(map[int32]bool)
				for _, ord := range tt.activeNodes {
					activeSet[ord] = true
				}

				reservedSet := make(map[int32]bool)
				for _, ord := range result.Spec.ReserveOrdinals {
					reservedSet[int32(ord.IntValue())] = true
				}

				// Verify: no reserved ordinal should be in activeNodes
				for _, ord := range result.Spec.ReserveOrdinals {
					ordVal := int32(ord.IntValue())
					assert.False(t, activeSet[ordVal],
						"reserveOrdinals contains %d which is in activeNodes - should be mutually exclusive", ordVal)
				}

				// Verify: no active ordinal should be in reserveOrdinals
				for _, ord := range tt.activeNodes {
					assert.False(t, reservedSet[ord],
						"activeNodes contains %d which is in reserveOrdinals - should be mutually exclusive", ord)
				}

				// Verify: every ordinal from 0 to maxOrdinal is in exactly one of the sets
				maxOrdinal := int32(0)
				for _, ord := range tt.activeNodes {
					if ord > maxOrdinal {
						maxOrdinal = ord
					}
				}

				for i := int32(0); i <= maxOrdinal; i++ {
					isActive := activeSet[i]
					isReserved := reservedSet[i]
					assert.True(t, isActive != isReserved,
						"ordinal %d should be in exactly one of activeNodes or reserveOrdinals (active=%v, reserved=%v)",
						i, isActive, isReserved)
				}
			}

			// Verify that with ephemeral nodes, OpenKruise will create pods only at activeNodes ordinals
			if tt.ephemeralNodes && len(tt.activeNodes) > 0 {
				t.Logf("With activeNodes=%v and reserveOrdinals=%v, OpenKruise will create pods at ordinals: %v",
					tt.activeNodes, tt.expectedReserveOrdinals, tt.activeNodes)

				// Verify that replicas + len(reserveOrdinals) = maxOrdinal + 1
				maxOrdinal := int32(0)
				for _, ord := range tt.activeNodes {
					if ord > maxOrdinal {
						maxOrdinal = ord
					}
				}
				expectedTotal := maxOrdinal + 1
				actualTotal := *result.Spec.Replicas + int32(len(result.Spec.ReserveOrdinals))
				assert.Equal(t, expectedTotal, actualTotal,
					"replicas + reserveOrdinals should equal maxOrdinal+1")
			}
		})
	}
}
