package controller

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/values"
)

func TestRenderDaemonSet(t *testing.T) {
	tests := []struct {
		name         string
		namespace    string
		clusterName  string
		controller   *values.SlurmController
		expectLabels map[string]string
		expectName   string
	}{
		{
			name:        "basic controller daemonset",
			namespace:   "test-namespace",
			clusterName: "test-cluster",
			controller: &values.SlurmController{
				SlurmNode: slurmv1.SlurmNode{
					K8sNodeFilterName: "test-filter",
				},
				DaemonSet: values.DaemonSet{
					Name: "test-controller-daemonset",
				},
				ContainerSlurmctld: values.Container{
					NodeContainer: slurmv1.NodeContainer{
						Image:           "test-image:latest",
						ImagePullPolicy: corev1.PullAlways,
						Port:            6817,
						AppArmorProfile: "unconfined",
					},
					Name: "slurmctld",
				},
				ContainerMunge: values.Container{
					NodeContainer: slurmv1.NodeContainer{
						Image:           "munge-image:latest",
						ImagePullPolicy: corev1.PullAlways,
						AppArmorProfile: "unconfined",
					},
				},
				PriorityClassName: "test-priority",
			},
			expectLabels: map[string]string{
				"app.kubernetes.io/component":     "controller",
				"app.kubernetes.io/instance":      "test-cluster",
				"slurm.nebius.ai/controller-type": "placeholder",
			},
			expectName: "test-controller-daemonset-placeholder",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			nodeFilters := []slurmv1.K8sNodeFilter{
				{
					Name: "test-filter",
					Affinity: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{
									{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{
												Key:      "node-type",
												Operator: corev1.NodeSelectorOpIn,
												Values:   []string{"controller"},
											},
										},
									},
								},
							},
						},
					},
				},
			}

			result := RenderDaemonSet(
				tt.namespace,
				tt.clusterName,
				nodeFilters,
				tt.controller,
			)

			// Check basic metadata
			if result.Name != tt.expectName {
				t.Errorf("DaemonSet name = %v, want %v", result.Name, tt.expectName)
			}

			if result.Namespace != tt.namespace {
				t.Errorf("DaemonSet namespace = %v, want %v", result.Namespace, tt.namespace)
			}

			// Check labels
			for key, expectedValue := range tt.expectLabels {
				if actualValue, exists := result.Labels[key]; !exists {
					t.Errorf("Expected label %s not found", key)
				} else if actualValue != expectedValue {
					t.Errorf("Label %s = %v, want %v", key, actualValue, expectedValue)
				}
			}

			// Check selector includes controller-type
			if result.Spec.Selector == nil {
				t.Error("DaemonSet selector is nil")
			} else {
				if controllerType, exists := result.Spec.Selector.MatchLabels[consts.LabelControllerType]; !exists {
					t.Error("DaemonSet selector missing controller-type label")
				} else if controllerType != consts.LabelControllerTypePlaceholder {
					t.Errorf("DaemonSet selector controller-type = %v, want %v", controllerType, consts.LabelControllerTypePlaceholder)
				}
			}

			// Check pod template labels
			podLabels := result.Spec.Template.Labels
			if controllerType, exists := podLabels[consts.LabelControllerType]; !exists {
				t.Error("Pod template missing controller-type label")
			} else if controllerType != consts.LabelControllerTypePlaceholder {
				t.Errorf("Pod template controller-type = %v, want %v", controllerType, consts.LabelControllerTypePlaceholder)
			}

			// Check priority class
			if result.Spec.Template.Spec.PriorityClassName != tt.controller.PriorityClassName {
				t.Errorf("PriorityClassName = %v, want %v", result.Spec.Template.Spec.PriorityClassName, tt.controller.PriorityClassName)
			}

			// Check update strategy
			if result.Spec.UpdateStrategy.Type != appsv1.RollingUpdateDaemonSetStrategyType {
				t.Errorf("UpdateStrategy type = %v, want %v", result.Spec.UpdateStrategy.Type, appsv1.RollingUpdateDaemonSetStrategyType)
			}

			if result.Spec.UpdateStrategy.RollingUpdate == nil {
				t.Error("RollingUpdate strategy is nil")
			} else {
				expectedMaxUnavailable := intstr.FromInt32(1)
				if result.Spec.UpdateStrategy.RollingUpdate.MaxUnavailable.IntVal != expectedMaxUnavailable.IntVal {
					t.Errorf("MaxUnavailable = %v, want %v", result.Spec.UpdateStrategy.RollingUpdate.MaxUnavailable.IntVal, expectedMaxUnavailable.IntVal)
				}
			}

			// Check containers - should use sleep versions
			containers := result.Spec.Template.Spec.Containers
			if len(containers) != 1 {
				t.Errorf("Expected 1 container, got %d", len(containers))
			} else {
				container := containers[0]
				if container.Name != consts.ContainerNameSlurmctld {
					t.Errorf("Container name = %v, want %v", container.Name, consts.ContainerNameSlurmctld)
				}
				if container.Image != tt.controller.ContainerSlurmctld.NodeContainer.Image {
					t.Errorf("Container image = %v, want %v", container.Image, tt.controller.ContainerSlurmctld.NodeContainer.Image)
				}
				// Check that it's using sleep command
				if len(container.Command) != 1 || container.Command[0] != "sleep" {
					t.Errorf("Container command = %v, want [sleep]", container.Command)
				}
				if len(container.Args) != 1 || container.Args[0] != "infinity" {
					t.Errorf("Container args = %v, want [infinity]", container.Args)
				}
				// Check that resources are not set (empty)
				if len(container.Resources.Limits) != 0 || len(container.Resources.Requests) != 0 {
					t.Errorf("Container resources should be empty, got limits: %v, requests: %v", container.Resources.Limits, container.Resources.Requests)
				}
			}

			// Check init containers - should also use sleep versions
			initContainers := result.Spec.Template.Spec.InitContainers
			if len(initContainers) != 1 {
				t.Errorf("Expected 1 init container, got %d", len(initContainers))
			} else {
				initContainer := initContainers[0]
				if initContainer.Name != consts.ContainerNameMunge {
					t.Errorf("Init container name = %v, want %v", initContainer.Name, consts.ContainerNameMunge)
				}
				if initContainer.Image != tt.controller.ContainerMunge.NodeContainer.Image {
					t.Errorf("Init container image = %v, want %v", initContainer.Image, tt.controller.ContainerMunge.NodeContainer.Image)
				}
				// Check that it's using sleep command
				if len(initContainer.Command) != 1 || initContainer.Command[0] != "sleep" {
					t.Errorf("Init container command = %v, want [sleep]", initContainer.Command)
				}
				if len(initContainer.Args) != 1 || initContainer.Args[0] != "infinity" {
					t.Errorf("Init container args = %v, want [infinity]", initContainer.Args)
				}
			}

			// Check that volumes are not set (empty) - DaemonSet doesn't need volumes
			if len(result.Spec.Template.Spec.Volumes) != 0 {
				t.Errorf("DaemonSet should have no volumes, got %d volumes", len(result.Spec.Template.Spec.Volumes))
			}
		})
	}
}

func TestRenderDaemonSetNodeAffinity(t *testing.T) {
	controller := &values.SlurmController{
		SlurmNode: slurmv1.SlurmNode{
			K8sNodeFilterName: "test-filter",
		},
		DaemonSet: values.DaemonSet{
			Name: "test-controller-daemonset",
		},
		ContainerSlurmctld: values.Container{
			NodeContainer: slurmv1.NodeContainer{
				Image:           "test-image:latest",
				ImagePullPolicy: corev1.PullAlways,
				AppArmorProfile: "unconfined",
			},
			Name: "slurmctld",
		},
		ContainerMunge: values.Container{
			NodeContainer: slurmv1.NodeContainer{
				Image:           "munge-image:latest",
				ImagePullPolicy: corev1.PullAlways,
				AppArmorProfile: "unconfined",
			},
		},
	}

	nodeFilters := []slurmv1.K8sNodeFilter{
		{
			Name: "test-filter",
			NodeSelector: map[string]string{
				"node-role": "controller",
			},
			Tolerations: []corev1.Toleration{
				{
					Key:      "node-role",
					Operator: corev1.TolerationOpEqual,
					Value:    "controller",
					Effect:   corev1.TaintEffectNoSchedule,
				},
			},
		},
	}

	result := RenderDaemonSet(
		"test-namespace",
		"test-cluster",
		nodeFilters,
		controller,
	)

	// Check node selector
	expectedNodeSelector := map[string]string{
		"node-role": "controller",
	}
	if len(result.Spec.Template.Spec.NodeSelector) != len(expectedNodeSelector) {
		t.Errorf("NodeSelector length = %d, want %d", len(result.Spec.Template.Spec.NodeSelector), len(expectedNodeSelector))
	}
	for key, expectedValue := range expectedNodeSelector {
		if actualValue, exists := result.Spec.Template.Spec.NodeSelector[key]; !exists {
			t.Errorf("NodeSelector key %s not found", key)
		} else if actualValue != expectedValue {
			t.Errorf("NodeSelector[%s] = %v, want %v", key, actualValue, expectedValue)
		}
	}

	// Check tolerations
	if len(result.Spec.Template.Spec.Tolerations) != 1 {
		t.Errorf("Expected 1 toleration, got %d", len(result.Spec.Template.Spec.Tolerations))
	} else {
		toleration := result.Spec.Template.Spec.Tolerations[0]
		if toleration.Key != "node-role" {
			t.Errorf("Toleration key = %v, want node-role", toleration.Key)
		}
		if toleration.Value != "controller" {
			t.Errorf("Toleration value = %v, want controller", toleration.Value)
		}
		if toleration.Effect != corev1.TaintEffectNoSchedule {
			t.Errorf("Toleration effect = %v, want %v", toleration.Effect, corev1.TaintEffectNoSchedule)
		}
	}
}
