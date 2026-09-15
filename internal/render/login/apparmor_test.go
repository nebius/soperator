package login

import (
	"testing"

	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/values"
)

func TestDefaultAppArmorProfile(t *testing.T) {
	tests := []struct {
		name       string
		useDefault bool
		custom     string
		expected   *corev1.AppArmorProfile
	}{
		{"default", true, "", &corev1.AppArmorProfile{Type: corev1.AppArmorProfileTypeLocalhost, LocalhostProfile: ptr.To("soperator-default")}},
		{"default takes precedence", true, "localhost/custom", &corev1.AppArmorProfile{Type: corev1.AppArmorProfileTypeLocalhost, LocalhostProfile: ptr.To("soperator-default")}},
		{"custom", false, "localhost/custom", &corev1.AppArmorProfile{Type: corev1.AppArmorProfileTypeLocalhost, LocalhostProfile: ptr.To("custom")}},
		{"raw custom", false, "custom", &corev1.AppArmorProfile{Type: corev1.AppArmorProfileTypeLocalhost, LocalhostProfile: ptr.To("custom")}},
		{"unconfined", false, "unconfined", &corev1.AppArmorProfile{Type: corev1.AppArmorProfileTypeUnconfined}},
		{"empty", false, "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			container := values.Container{NodeContainer: slurmv1.NodeContainer{
				AppArmorProfile: tt.custom,
				Resources: corev1.ResourceList{
					corev1.ResourceCPU:              resource.MustParse("100m"),
					corev1.ResourceMemory:           resource.MustParse("1Gi"),
					corev1.ResourceEphemeralStorage: resource.MustParse("1Gi"),
				},
			}}
			login := &values.SlurmLogin{
				SlurmNode:                 slurmv1.SlurmNode{K8sNodeFilterName: "test"},
				ContainerSshd:             container,
				ContainerMunge:            container,
				UseDefaultAppArmorProfile: tt.useDefault,
				VolumeJail:                slurmv1.NodeVolume{VolumeSourceName: ptr.To("jail")},
			}
			filters := []slurmv1.K8sNodeFilter{{Name: "test"}}
			volumes := []slurmv1.VolumeSource{{Name: "jail", VolumeSource: corev1.VolumeSource{EmptyDir: &corev1.EmptyDirVolumeSource{}}}}
			sts, err := RenderStatefulSet("namespace", "cluster", false, filters, &slurmv1.Secrets{}, volumes, login, nil)
			require.NoError(t, err)
			require.Equal(t, tt.expected, sts.Spec.Template.Spec.Containers[0].SecurityContext.AppArmorProfile)
		})
	}
}
