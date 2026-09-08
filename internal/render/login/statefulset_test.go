package login

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/values"
)

func TestRenderStatefulSet_PriorityClass(t *testing.T) {
	tests := []struct {
		name          string
		priorityClass string
		expectedClass string
	}{
		{
			name:          "empty priority class",
			priorityClass: "",
			expectedClass: "",
		},
		{
			name:          "custom priority class",
			priorityClass: "high-priority",
			expectedClass: "high-priority",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup test data
			namespace := "test-namespace"
			clusterName := "test-cluster"
			nodeFilters := []slurmv1.K8sNodeFilter{
				{
					Name: "test-filter",
				},
			}
			secrets := &slurmv1.Secrets{}
			volumeSources := []slurmv1.VolumeSource{
				{
					Name: "test-volume",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{},
					},
				},
			}

			// Complete login configuration
			login := &values.SlurmLogin{
				SlurmNode: slurmv1.SlurmNode{
					K8sNodeFilterName: "test-filter",
					PriorityClass:     tt.priorityClass,
				},
				ContainerSshd: values.Container{
					NodeContainer: slurmv1.NodeContainer{
						Image:           "test-sshd-image",
						ImagePullPolicy: corev1.PullIfNotPresent,
						Port:            22,
						Resources: corev1.ResourceList{
							corev1.ResourceMemory:           resource.MustParse("1Gi"),
							corev1.ResourceCPU:              resource.MustParse("100m"),
							corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
						},
					},
				},
				ContainerMunge: values.Container{
					NodeContainer: slurmv1.NodeContainer{
						Image:           "test-munge-image",
						ImagePullPolicy: corev1.PullIfNotPresent,
						Resources: corev1.ResourceList{
							corev1.ResourceMemory:           resource.MustParse("1Gi"),
							corev1.ResourceCPU:              resource.MustParse("100m"),
							corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
						},
					},
				},
				VolumeJail: slurmv1.NodeVolume{
					VolumeSourceName: &[]string{"test-volume"}[0],
				},
				StatefulSet: values.StatefulSet{
					Name:     "test-login",
					Replicas: 1,
				},
				HeadlessService: values.Service{
					Name: "test-headless",
				},
				SSHDConfigMapName:    "test-sshd-config",
				CustomInitContainers: []corev1.Container{},
				JailSubMounts:        []slurmv1.NodeVolumeMount{},
				CustomVolumeMounts:   []slurmv1.NodeVolumeMount{},
			}

			// Render StatefulSet
			result, err := RenderStatefulSet(
				namespace,
				clusterName,
				true,
				nodeFilters,
				secrets,
				volumeSources,
				login,
			)

			if err != nil {
				t.Fatalf("RenderStatefulSet() error = %v", err)
			}

			// Check PriorityClassName
			if result.Spec.Template.Spec.PriorityClassName != tt.expectedClass {
				t.Errorf("PriorityClassName = %v, want %v", result.Spec.Template.Spec.PriorityClassName, tt.expectedClass)
			}
		})
	}
}

func TestRenderStatefulSet_CustomLabelsAndAnnotations(t *testing.T) {
	namespace := "test-namespace"
	clusterName := "test-cluster"
	nodeFilters := []slurmv1.K8sNodeFilter{{Name: "test-filter"}}
	secrets := &slurmv1.Secrets{}
	volumeSources := []slurmv1.VolumeSource{
		{
			Name:         "test-volume",
			VolumeSource: corev1.VolumeSource{HostPath: &corev1.HostPathVolumeSource{}},
		},
	}

	login := &values.SlurmLogin{
		SlurmNode: slurmv1.SlurmNode{
			K8sNodeFilterName: "test-filter",
		},
		Labels:      map[string]string{"gcore.com/project-id": "123"},
		Annotations: map[string]string{"gcore.com/note": "abc"},
		ContainerSshd: values.Container{
			NodeContainer: slurmv1.NodeContainer{
				Image: "test-sshd-image",
				Port:  22,
				Resources: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("1Gi"),
					corev1.ResourceCPU:    resource.MustParse("100m"),
				},
			},
		},
		ContainerMunge: values.Container{
			NodeContainer: slurmv1.NodeContainer{
				Image: "test-munge-image",
				Resources: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse("1Gi"),
					corev1.ResourceCPU:    resource.MustParse("100m"),
				},
			},
		},
		VolumeJail:      slurmv1.NodeVolume{VolumeSourceName: &[]string{"test-volume"}[0]},
		StatefulSet:     values.StatefulSet{Name: "test-login", Replicas: 1},
		HeadlessService: values.Service{Name: "test-headless"},
	}

	result, err := RenderStatefulSet(namespace, clusterName, true, nodeFilters, secrets, volumeSources, login)
	if err != nil {
		t.Fatalf("RenderStatefulSet() error = %v", err)
	}

	if got := result.Labels["gcore.com/project-id"]; got != "123" {
		t.Errorf("StatefulSet label = %v, want 123", got)
	}
	if got := result.Spec.Template.Labels["gcore.com/project-id"]; got != "123" {
		t.Errorf("Pod template label = %v, want 123", got)
	}
	if got := result.Spec.Template.Annotations["gcore.com/note"]; got != "abc" {
		t.Errorf("Pod template annotation = %v, want abc", got)
	}
}
