package sconfigcontroller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/values"
)

func TestRenderContainerSConfigController(t *testing.T) {
	tests := []struct {
		name             string
		clusterNamespace string
		clusterName      string
		slurmAPIServer   string
		jailConfigPath   string
		container        values.Container
		expectedCommand  []string
		expectedArgs     []string
		expectedImage    string
	}{
		{
			name:             "Default configuration",
			clusterNamespace: "default",
			clusterName:      "test-cluster",
			slurmAPIServer:   "http://slurm-api-server",
			jailConfigPath:   "/etc/slurm",
			container: values.Container{
				Name: "test-container",
				NodeContainer: slurmv1.NodeContainer{
					Image:           "test-image:latest",
					ImagePullPolicy: corev1.PullIfNotPresent,
					Resources:       corev1.ResourceList{},
				},
			},
			expectedCommand: []string{"/usr/bin/sconfigcontroller"},
			expectedArgs: []string{
				"--cluster-namespace=default",
				"--cluster-name=test-cluster",
				"--jail-path=/mnt/jail",
				"--slurmapiserver=http://slurm-api-server",
				"--leader-elect",
			},
			expectedImage: "test-image:latest",
		},
		{
			name:             "Custom configuration",
			clusterNamespace: "custom-namespace",
			clusterName:      "custom-cluster",
			slurmAPIServer:   "https://custom-slurm-api",
			jailConfigPath:   "/custom/path",
			container: values.Container{
				Name: "custom-container",
				NodeContainer: slurmv1.NodeContainer{
					Image:           "custom-image:v1",
					ImagePullPolicy: corev1.PullAlways,
					Resources:       corev1.ResourceList{},
				},
			},
			expectedCommand: []string{"/usr/bin/sconfigcontroller"},
			expectedArgs: []string{
				"--cluster-namespace=custom-namespace",
				"--cluster-name=custom-cluster",
				"--jail-path=/mnt/jail",
				"--slurmapiserver=https://custom-slurm-api",
				"--leader-elect",
			},
			expectedImage: "custom-image:v1",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Call the function
			result := renderContainerSConfigController(
				tt.clusterNamespace,
				tt.clusterName,
				tt.slurmAPIServer,
				tt.container,
			)

			// Validate the result
			if result.Name != consts.ContainerNameSConfigController {
				t.Errorf("expected container name %s, got %s", consts.ContainerNameSConfigController, result.Name)
			}
			if result.Image != tt.expectedImage {
				t.Errorf("expected image %s, got %s", tt.expectedImage, result.Image)
			}
			if result.ImagePullPolicy != tt.container.ImagePullPolicy {
				t.Errorf("expected image pull policy %s, got %s", tt.container.ImagePullPolicy, result.ImagePullPolicy)
			}
			if !equalSlices(result.Command, tt.expectedCommand) {
				t.Errorf("expected command %v, got %v", tt.expectedCommand, result.Command)
			}
			if !equalSlices(result.Args, tt.expectedArgs) {
				t.Errorf("expected args %v, got %v", tt.expectedArgs, result.Args)
			}
		})
	}
}

// Helper function to compare slices
func equalSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
