package controller

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/values"
)

func TestRenderStatefulSet(t *testing.T) {
	tests := []struct {
		name           string
		namespace      string
		clusterName    string
		controller     *values.SlurmController
		expectLabels   map[string]string
		expectName     string
		expectReplicas int32
	}{
		{
			name:        "basic controller statefulset",
			namespace:   "test-namespace",
			clusterName: "test-cluster",
			controller: &values.SlurmController{
				K8sNodeFilterName: "test-filter",
				StatefulSet: values.StatefulSet{
					Name:           "test-controller-sts",
					Replicas:       1,
					MaxUnavailable: intstr.FromInt32(1),
				},
				Service: values.Service{
					Name: "test-controller-svc",
				},
				ContainerSlurmctld: values.Container{
					NodeContainer: slurmv1.NodeContainer{
						Image:           "test-image:latest",
						ImagePullPolicy: corev1.PullAlways,
						Port:            6817,
						Resources: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("500m"),
							corev1.ResourceMemory: resource.MustParse("1Gi"),
						},
						AppArmorProfile: consts.AppArmorProfileUnconfined,
					},
					Name: "slurmctld",
				},
				ContainerMunge: values.Container{
					NodeContainer: slurmv1.NodeContainer{
						Image:           "munge-image:latest",
						ImagePullPolicy: corev1.PullAlways,
						Resources: corev1.ResourceList{
							corev1.ResourceCPU:    resource.MustParse("100m"),
							corev1.ResourceMemory: resource.MustParse("256Mi"),
						},
						AppArmorProfile: consts.AppArmorProfileUnconfined,
					},
				},
				VolumeSpool: slurmv1.NodeVolume{
					VolumeSourceName: ptr.To("test-volume"),
				},
				VolumeJail: slurmv1.NodeVolume{
					VolumeSourceName: ptr.To("test-volume"),
				},
				PriorityClass: "test-priority",
			},
			expectLabels: map[string]string{
				"app.kubernetes.io/component":     "controller",
				"app.kubernetes.io/instance":      "test-cluster",
				"slurm.nebius.ai/controller-type": "main",
			},
			expectName:     "test-controller-sts",
			expectReplicas: 1,
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

			volumeSources := []slurmv1.VolumeSource{
				{
					Name: "test-volume",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
			}

			result, err := RenderStatefulSet(
				tt.namespace,
				tt.clusterName,
				nodeFilters,
				volumeSources,
				tt.controller,
			)

			if err != nil {
				t.Fatalf("RenderStatefulSet() error = %v", err)
			}

			// Check basic metadata
			if result.Name != tt.expectName {
				t.Errorf("StatefulSet name = %v, want %v", result.Name, tt.expectName)
			}

			if result.Namespace != tt.namespace {
				t.Errorf("StatefulSet namespace = %v, want %v", result.Namespace, tt.namespace)
			}

			// Check labels
			for key, expectedValue := range tt.expectLabels {
				if actualValue, exists := result.Labels[key]; !exists {
					t.Errorf("Expected label %s not found", key)
				} else if actualValue != expectedValue {
					t.Errorf("Label %s = %v, want %v", key, actualValue, expectedValue)
				}
			}

			// Check replicas
			if *result.Spec.Replicas != tt.expectReplicas {
				t.Errorf("StatefulSet replicas = %v, want %v", *result.Spec.Replicas, tt.expectReplicas)
			}

			// Check selector includes controller-type
			if result.Spec.Selector == nil {
				t.Error("StatefulSet selector is nil")
			} else {
				if controllerType, exists := result.Spec.Selector.MatchLabels[consts.LabelControllerType]; !exists {
					t.Error("StatefulSet selector missing controller-type label")
				} else if controllerType != consts.LabelControllerTypeMain {
					t.Errorf("StatefulSet selector controller-type = %v, want %v", controllerType, consts.LabelControllerTypeMain)
				}
			}

			// Check pod template labels
			podLabels := result.Spec.Template.Labels
			if controllerType, exists := podLabels[consts.LabelControllerType]; !exists {
				t.Error("Pod template missing controller-type label")
			} else if controllerType != consts.LabelControllerTypeMain {
				t.Errorf("Pod template controller-type = %v, want %v", controllerType, consts.LabelControllerTypeMain)
			}

			// Check priority class
			if result.Spec.Template.Spec.PriorityClassName != tt.controller.PriorityClass {
				t.Errorf("PriorityClassName = %v, want %v", result.Spec.Template.Spec.PriorityClassName, tt.controller.PriorityClass)
			}

			// Check containers
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
			}

			// Check init containers
			initContainers := result.Spec.Template.Spec.InitContainers
			if len(initContainers) != 2 {
				t.Errorf("Expected 2 init containers, got %d", len(initContainers))
			} else {
				initContainer := initContainers[0]
				if initContainer.Name != consts.ContainerNameMunge {
					t.Errorf("Init container name = %v, want %v", initContainer.Name, consts.ContainerNameMunge)
				}
				if initContainer.Image != tt.controller.ContainerMunge.NodeContainer.Image {
					t.Errorf("Init container image = %v, want %v", initContainer.Image, tt.controller.ContainerMunge.NodeContainer.Image)
				}

				initContainer = initContainers[1]
				if initContainer.Name != consts.ContainerNameWaitForAccounting {
					t.Errorf("Init container name = %v, want %v", initContainer.Name, consts.ContainerNameWaitForAccounting)
				}
				if initContainer.Image != tt.controller.ContainerSlurmctld.NodeContainer.Image {
					t.Errorf("Init container image = %v, want %v", initContainer.Image, tt.controller.ContainerSlurmctld.NodeContainer.Image)
				}
			}
		})
	}
}

func TestRenderStatefulSetWithMaintenance(t *testing.T) {
	maintenanceMode := consts.MaintenanceMode("enabled")
	controller := &values.SlurmController{
		K8sNodeFilterName: "test-filter",
		StatefulSet: values.StatefulSet{
			Name:           "test-controller-sts",
			Replicas:       1,
			MaxUnavailable: intstr.FromInt32(1),
		},
		Service: values.Service{
			Name: "test-controller-svc",
		},
		ContainerSlurmctld: values.Container{
			NodeContainer: slurmv1.NodeContainer{
				Image:           "test-image:latest",
				ImagePullPolicy: corev1.PullAlways,
				Port:            6817,
				AppArmorProfile: consts.AppArmorProfileUnconfined,
			},
			Name: "slurmctld",
		},
		ContainerMunge: values.Container{
			NodeContainer: slurmv1.NodeContainer{
				Image:           "munge-image:latest",
				ImagePullPolicy: corev1.PullAlways,
				AppArmorProfile: consts.AppArmorProfileUnconfined,
			},
		},
		VolumeSpool: slurmv1.NodeVolume{
			VolumeSourceName: ptr.To("test-volume"),
		},
		VolumeJail: slurmv1.NodeVolume{
			VolumeSourceName: ptr.To("test-volume"),
		},
		Maintenance: &maintenanceMode,
	}

	nodeFilters := []slurmv1.K8sNodeFilter{
		{
			Name: "test-filter",
		},
	}

	result, err := RenderStatefulSet(
		"test-namespace",
		"test-cluster",
		nodeFilters,
		[]slurmv1.VolumeSource{
			{
				Name: "test-volume",
				VolumeSource: corev1.VolumeSource{
					EmptyDir: &corev1.EmptyDirVolumeSource{},
				},
			},
		},
		controller,
	)

	if err != nil {
		t.Fatalf("RenderStatefulSet() error = %v", err)
	}

	// Check that replicas is set to 0 during maintenance
	if *result.Spec.Replicas != 0 {
		t.Errorf("Expected 0 replicas during maintenance, got %d", *result.Spec.Replicas)
	}
}

func TestRenderStatefulSetHostUsers(t *testing.T) {
	tests := []struct {
		name              string
		hostUsers         *bool
		expectedHostUsers *bool
	}{
		{
			name:              "hostUsers not set (nil)",
			hostUsers:         nil,
			expectedHostUsers: nil,
		},
		{
			name:              "hostUsers set to false",
			hostUsers:         ptr.To(false),
			expectedHostUsers: ptr.To(false),
		},
		{
			name:              "hostUsers set to true",
			hostUsers:         ptr.To(true),
			expectedHostUsers: ptr.To(true),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			controller := &values.SlurmController{
				K8sNodeFilterName: "test-filter",
				HostUsers:         tt.hostUsers,
				StatefulSet: values.StatefulSet{
					Name:           "test-controller-sts",
					Replicas:       1,
					MaxUnavailable: intstr.FromInt32(1),
				},
				Service: values.Service{
					Name: "test-controller-svc",
				},
				ContainerSlurmctld: values.Container{
					NodeContainer: slurmv1.NodeContainer{
						Image:           "test-image:latest",
						ImagePullPolicy: corev1.PullAlways,
						Port:            6817,
						AppArmorProfile: consts.AppArmorProfileUnconfined,
					},
					Name: "slurmctld",
				},
				ContainerMunge: values.Container{
					NodeContainer: slurmv1.NodeContainer{
						Image:           "munge-image:latest",
						ImagePullPolicy: corev1.PullAlways,
						AppArmorProfile: consts.AppArmorProfileUnconfined,
					},
				},
				VolumeSpool: slurmv1.NodeVolume{
					VolumeSourceName: ptr.To("test-volume"),
				},
				VolumeJail: slurmv1.NodeVolume{
					VolumeSourceName: ptr.To("test-volume"),
				},
			}

			nodeFilters := []slurmv1.K8sNodeFilter{
				{
					Name: "test-filter",
				},
			}

			volumeSources := []slurmv1.VolumeSource{
				{
					Name: "test-volume",
					VolumeSource: corev1.VolumeSource{
						EmptyDir: &corev1.EmptyDirVolumeSource{},
					},
				},
			}

			result, err := RenderStatefulSet(
				"test-namespace",
				"test-cluster",
				nodeFilters,
				volumeSources,
				controller,
			)

			if err != nil {
				t.Fatalf("RenderStatefulSet() error = %v", err)
			}

			// Check HostUsers field in PodSpec
			actualHostUsers := result.Spec.Template.Spec.HostUsers

			if tt.expectedHostUsers == nil {
				if actualHostUsers != nil {
					t.Errorf("Expected HostUsers to be nil, got %v", *actualHostUsers)
				}
			} else {
				if actualHostUsers == nil {
					t.Errorf("Expected HostUsers to be %v, got nil", *tt.expectedHostUsers)
				} else if *actualHostUsers != *tt.expectedHostUsers {
					t.Errorf("Expected HostUsers to be %v, got %v", *tt.expectedHostUsers, *actualHostUsers)
				}
			}
		})
	}
}
