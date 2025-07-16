package sconfigcontroller_test

import (
	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"
	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	. "nebius.ai/slurm-operator/internal/render/sconfigcontroller"
	"nebius.ai/slurm-operator/internal/values"

	"testing"
)

func TestRenderDeployment(t *testing.T) {
	tests := []struct {
		name              string
		sConfigController *values.SConfigController
	}{
		{
			name: "Basic SConfigController",
			sConfigController: &values.SConfigController{
				SlurmNode: slurmv1.SlurmNode{
					Size:              1,
					K8sNodeFilterName: "test",
				},
				Container: values.Container{
					Name: "test-container",
					NodeContainer: slurmv1.NodeContainer{
						Image:           "nginx:latest",
						ImagePullPolicy: corev1.PullIfNotPresent,
					},
				},
				Maintenance: consts.MaintenanceMode(consts.ModeNone),
				VolumeJail: slurmv1.NodeVolume{
					VolumeSourceName: ptr.To("test-volume-source"),
				},
				JailSlurmConfigPath: "/etc/slurm",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Call the function
			deployment, err := RenderDeployment(
				"test-namespace",
				"test-cluster",
				"http://slurm-api-server",
				tt.sConfigController,
				[]slurmv1.K8sNodeFilter{
					{
						Name: "test",
						NodeSelector: map[string]string{
							"test-node-selector": "test-node-selector-value",
						},
					},
				},
				[]slurmv1.VolumeSource{
					{
						Name: "test-volume-source",
						VolumeSource: corev1.VolumeSource{
							HostPath: &corev1.HostPathVolumeSource{},
						},
					},
				},
			)
			if err != nil {
				t.Fatalf("RenderDeployment returned an error: %v", err)
			}

			// Validate the result
			if deployment == nil {
				t.Fatalf("RenderDeployment returned nil")
			}

			// Check container image
			if len(deployment.Spec.Template.Spec.Containers) == 0 {
				t.Fatalf("expected at least one container, got none")
			}
			container := deployment.Spec.Template.Spec.Containers[0]
			if container.Image != tt.sConfigController.Container.Image {
				t.Errorf("expected image %s, got %s", tt.sConfigController.Container.Image, container.Image)
			}
		})
	}
}
