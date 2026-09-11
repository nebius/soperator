package login

import (
	"reflect"
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
			namespace := "test-namespace"
			clusterName := "test-cluster"
			nodeFilters := []slurmv1.K8sNodeFilter{{Name: "test-filter"}}
			secrets := &slurmv1.Secrets{}
			volumeSources := []slurmv1.VolumeSource{{
				Name: "test-volume",
				VolumeSource: corev1.VolumeSource{
					HostPath: &corev1.HostPathVolumeSource{},
				},
			}}

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
				HeadlessService:      values.Service{Name: "test-headless"},
				SSHDConfigMapName:    "test-sshd-config",
				CustomInitContainers: []corev1.Container{},
				JailSubMounts:        []slurmv1.NodeVolumeMount{},
				CustomVolumeMounts:   []slurmv1.NodeVolumeMount{},
			}

			desiredReplicas := login.StatefulSet.Replicas
			result, err := RenderStatefulSet(
				namespace,
				clusterName,
				true,
				nodeFilters,
				secrets,
				volumeSources,
				login,
				&desiredReplicas,
			)
			if err != nil {
				t.Fatalf("RenderStatefulSet() error = %v", err)
			}

			if result.Spec.Template.Spec.PriorityClassName != tt.expectedClass {
				t.Errorf("PriorityClassName = %v, want %v", result.Spec.Template.Spec.PriorityClassName, tt.expectedClass)
			}
			if result.Spec.ServiceName != login.HeadlessService.Name {
				t.Errorf("ServiceName = %q, want %q", result.Spec.ServiceName, login.HeadlessService.Name)
			}
			if result.Spec.Replicas == nil || *result.Spec.Replicas != desiredReplicas {
				t.Errorf("Replicas = %v, want %d", result.Spec.Replicas, desiredReplicas)
			}

			sshd := result.Spec.Template.Spec.Containers[0]
			if sshd.Lifecycle == nil || sshd.Lifecycle.PreStop == nil || sshd.Lifecycle.PreStop.Exec == nil {
				t.Fatal("SSHD container preStop exec hook is not configured")
			}
			if got, want := sshd.Lifecycle.PreStop.Exec.Command, []string{"/bin/sh", "-c", "sleep 15"}; !reflect.DeepEqual(got, want) {
				t.Errorf("SSHD container preStop command = %v, want %v", got, want)
			}

			externallyScaled, err := RenderStatefulSet(
				namespace,
				clusterName,
				true,
				nodeFilters,
				secrets,
				volumeSources,
				login,
				nil,
			)
			if err != nil {
				t.Fatalf("RenderStatefulSet() with externally owned replicas error = %v", err)
			}
			if externallyScaled.Spec.Replicas != nil {
				t.Errorf("externally owned Replicas = %v, want nil", externallyScaled.Spec.Replicas)
			}

			zeroReplicas := int32(0)
			scaledToZero, err := RenderStatefulSet(
				namespace,
				clusterName,
				true,
				nodeFilters,
				secrets,
				volumeSources,
				login,
				&zeroReplicas,
			)
			if err != nil {
				t.Fatalf("RenderStatefulSet() with zero replicas error = %v", err)
			}
			if scaledToZero.Spec.Replicas == nil || *scaledToZero.Spec.Replicas != zeroReplicas {
				t.Errorf("zero Replicas = %v, want %d", scaledToZero.Spec.Replicas, zeroReplicas)
			}
		})
	}
}
