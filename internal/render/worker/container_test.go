package worker

import (
	"reflect"
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/values"
)

func Test_RenderContainerSlurmd(t *testing.T) {
	imageName := "test-image"
	containerName := "test-container"

	tests := []struct {
		name        string
		container   *values.Container
		features    []slurmv1.WorkerFeature
		wantLimits  corev1.ResourceList
		wantReqs    corev1.ResourceList
		wantEnvVars map[string]string
	}{
		{
			name: "With Resources and features",
			container: &values.Container{
				NodeContainer: slurmv1.NodeContainer{
					Image:           imageName,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Port:            8080,
					Resources: corev1.ResourceList{
						corev1.ResourceMemory:           resource.MustParse("1Gi"),
						corev1.ResourceCPU:              resource.MustParse("100m"),
						corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
					},
				},
				Name: containerName,
			},
			features: []slurmv1.WorkerFeature{
				{
					Name:         "f42",
					HostlistExpr: "worker-[0-10]",
				},
			},
			wantLimits: corev1.ResourceList{
				corev1.ResourceMemory:           resource.MustParse("1Gi"),
				corev1.ResourceCPU:              resource.MustParse("100m"),
				corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
			},
			wantReqs: corev1.ResourceList{
				corev1.ResourceMemory:           resource.MustParse("1Gi"),
				corev1.ResourceCPU:              resource.MustParse("100m"),
				corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
			},
			wantEnvVars: map[string]string{
				"SLURM_FEATURE_f42": "worker-[0-10]",
			},
		},
		{
			name: "Without Resources",
			container: &values.Container{
				NodeContainer: slurmv1.NodeContainer{
					Image:           imageName,
					ImagePullPolicy: corev1.PullIfNotPresent,
					Port:            8080,
				},
				Name: containerName,
			},
			wantLimits: nil,
			wantReqs:   nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := renderContainerSlurmd(
				tt.container,
				nil,
				nil,
				"test-cluster",
				consts.ClusterTypeGPU,
				"v1",
				false,
				"{ \"monitoring\": \"https://my-cloud.com/$INSTANCE_ID/monitoring\" }",
				tt.features,
				"default",
				false,
			)
			if err != nil && tt.wantLimits != nil {
				t.Errorf("renderContainerSlurmd() error = %v, want nil", err)
			}
			if !reflect.DeepEqual(got.Resources.Limits, tt.wantLimits) {
				t.Errorf("renderContainerSlurmd() Limits = %v, want %v", got.Resources.Limits, tt.wantLimits)
			}
			if !reflect.DeepEqual(got.Resources.Requests, tt.wantReqs) {
				t.Errorf("renderContainerSlurmd() Requests = %v, want %v", got.Resources.Requests, tt.wantReqs)
			}
			gotEnvVars := make(map[string]string, len(got.Env))
			for _, env := range got.Env {
				gotEnvVars[env.Name] = env.Value
			}
			for key, value := range tt.wantEnvVars {
				assert.Equal(t, value, gotEnvVars[key], "env var for key %q mismatch", key)
			}
		})
	}
}

func TestRenderContainerWaitForTopology(t *testing.T) {
	imageName := "test-image"

	tests := []struct {
		name               string
		container          *values.Container
		waitTimeoutSeconds int32
		expectedEnvVars    map[string]string
	}{
		{
			name: "default timeout",
			container: &values.Container{
				NodeContainer: slurmv1.NodeContainer{
					Image:           imageName,
					ImagePullPolicy: corev1.PullIfNotPresent,
				},
			},
			waitTimeoutSeconds: 300,
			expectedEnvVars: map[string]string{
				"TOPOLOGY_CONFIGMAP_PATH": consts.VolumeMountPathTopologyNodeLabels,
				"TOPOLOGY_ENV_FILE":       consts.TopologyEnvFilePath,
				"TOPOLOGY_WAIT_TIMEOUT":   "300",
				"TOPOLOGY_POLL_INTERVAL":  "5",
			},
		},
		{
			name: "custom timeout",
			container: &values.Container{
				NodeContainer: slurmv1.NodeContainer{
					Image:           imageName,
					ImagePullPolicy: corev1.PullAlways,
				},
			},
			waitTimeoutSeconds: 600,
			expectedEnvVars: map[string]string{
				"TOPOLOGY_WAIT_TIMEOUT": "600",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := RenderContainerWaitForTopology(tt.container, tt.waitTimeoutSeconds)

			// Verify container basics
			assert.Equal(t, consts.ContainerNameWaitForTopology, got.Name)
			assert.Equal(t, tt.container.Image, got.Image)
			assert.Equal(t, tt.container.ImagePullPolicy, got.ImagePullPolicy)
			assert.Equal(t, []string{"python3", "/opt/bin/slurm/wait_for_topology.py"}, got.Command)

			// Verify volume mounts
			assert.Len(t, got.VolumeMounts, 2)
			expectedMounts := map[string]struct {
				path     string
				readOnly bool
			}{
				consts.VolumeNameTopologyNodeLabels: {consts.VolumeMountPathTopologyNodeLabels, true},
				consts.VolumeNameTopologyEnv:        {consts.VolumeMountPathTopologyEnv, false},
			}
			for _, mount := range got.VolumeMounts {
				expected, exists := expectedMounts[mount.Name]
				assert.True(t, exists, "unexpected volume mount: %s", mount.Name)
				assert.Equal(t, expected.path, mount.MountPath, "wrong mount path for %s", mount.Name)
				assert.Equal(t, expected.readOnly, mount.ReadOnly, "wrong readOnly for %s", mount.Name)
			}

			// Verify env vars
			gotEnvVars := make(map[string]string, len(got.Env))
			for _, env := range got.Env {
				if env.ValueFrom != nil {
					// Skip env vars with ValueFrom (like K8S_NODE_NAME)
					continue
				}
				gotEnvVars[env.Name] = env.Value
			}
			for key, value := range tt.expectedEnvVars {
				assert.Equal(t, value, gotEnvVars[key], "env var for key %q mismatch", key)
			}

			// Verify K8S_NODE_NAME env var with FieldRef
			var foundNodeName bool
			for _, env := range got.Env {
				if env.Name == "K8S_NODE_NAME" {
					foundNodeName = true
					assert.NotNil(t, env.ValueFrom)
					assert.NotNil(t, env.ValueFrom.FieldRef)
					assert.Equal(t, "spec.nodeName", env.ValueFrom.FieldRef.FieldPath)
				}
			}
			assert.True(t, foundNodeName, "K8S_NODE_NAME env var not found")
		})
	}
}
