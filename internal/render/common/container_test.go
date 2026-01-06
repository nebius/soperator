package common

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"k8s.io/utils/ptr"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/values"
)

func TestRenderContainerMunge_AppArmorProfile(t *testing.T) {
	tests := []struct {
		name                    string
		appArmorProfile         string
		expectedAppArmorProfile *corev1.AppArmorProfile
		description             string
	}{
		{
			name:            "unconfined profile",
			appArmorProfile: "unconfined",
			expectedAppArmorProfile: &corev1.AppArmorProfile{
				Type: corev1.AppArmorProfileTypeUnconfined,
			},
			description: "should set AppArmorProfile to Unconfined",
		},
		{
			name:            "localhost profile",
			appArmorProfile: "localhost/slurm-profile",
			expectedAppArmorProfile: &corev1.AppArmorProfile{
				Type:             corev1.AppArmorProfileTypeLocalhost,
				LocalhostProfile: ptr.To("slurm-profile"),
			},
			description: "should set AppArmorProfile to Localhost with profile name",
		},
		{
			name:                    "empty profile",
			appArmorProfile:         "",
			expectedAppArmorProfile: nil,
			description:             "should set AppArmorProfile to nil when empty",
		},
		{
			name:            "profile without localhost prefix",
			appArmorProfile: "my-profile",
			expectedAppArmorProfile: &corev1.AppArmorProfile{
				Type:             corev1.AppArmorProfileTypeLocalhost,
				LocalhostProfile: ptr.To("my-profile"),
			},
			description: "should default to localhost type",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			container := &values.Container{
				NodeContainer: slurmv1.NodeContainer{
					Image:           "test-image:latest",
					AppArmorProfile: tt.appArmorProfile,
					Resources: corev1.ResourceList{
						corev1.ResourceCPU:    resource.MustParse("100m"),
						corev1.ResourceMemory: resource.MustParse("128Mi"),
					},
				},
			}

			result := RenderContainerMunge(container)

			if result.Name != consts.ContainerNameMunge {
				t.Errorf("Expected container name to be %s, got %s", consts.ContainerNameMunge, result.Name)
			}

			if result.SecurityContext == nil {
				t.Fatal("SecurityContext should not be nil")
			}

			actualProfile := result.SecurityContext.AppArmorProfile

			if tt.expectedAppArmorProfile == nil {
				if actualProfile != nil {
					t.Errorf("Expected AppArmorProfile to be nil, got %v", actualProfile)
				}
				return
			}

			if actualProfile == nil {
				t.Fatalf("Expected AppArmorProfile to be %v, got nil", tt.expectedAppArmorProfile)
			}

			if actualProfile.Type != tt.expectedAppArmorProfile.Type {
				t.Errorf("Expected AppArmorProfile.Type to be %v, got %v",
					tt.expectedAppArmorProfile.Type, actualProfile.Type)
			}

			if tt.expectedAppArmorProfile.LocalhostProfile != nil {
				if actualProfile.LocalhostProfile == nil {
					t.Errorf("Expected LocalhostProfile to be %v, got nil",
						*tt.expectedAppArmorProfile.LocalhostProfile)
				} else if *actualProfile.LocalhostProfile != *tt.expectedAppArmorProfile.LocalhostProfile {
					t.Errorf("Expected LocalhostProfile to be %v, got %v",
						*tt.expectedAppArmorProfile.LocalhostProfile, *actualProfile.LocalhostProfile)
				}
			} else if actualProfile.LocalhostProfile != nil {
				t.Errorf("Expected LocalhostProfile to be nil, got %v",
					*actualProfile.LocalhostProfile)
			}
		})
	}
}

func TestRenderContainerMunge_SecurityContext(t *testing.T) {
	container := &values.Container{
		NodeContainer: slurmv1.NodeContainer{
			Image:           "test-image:latest",
			AppArmorProfile: "unconfined",
			Resources: corev1.ResourceList{
				corev1.ResourceCPU:    resource.MustParse("100m"),
				corev1.ResourceMemory: resource.MustParse("128Mi"),
			},
		},
	}

	result := RenderContainerMunge(container)

	if result.SecurityContext == nil {
		t.Fatal("SecurityContext should not be nil")
	}

	// Check capabilities
	if result.SecurityContext.Capabilities == nil {
		t.Fatal("Capabilities should not be nil")
	}

	expectedCap := corev1.Capability(consts.ContainerSecurityContextCapabilitySysAdmin)
	found := false
	for _, cap := range result.SecurityContext.Capabilities.Add {
		if cap == expectedCap {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected capability %s to be added", expectedCap)
	}

	// Check AppArmorProfile
	if result.SecurityContext.AppArmorProfile == nil {
		t.Error("AppArmorProfile should not be nil")
	} else if result.SecurityContext.AppArmorProfile.Type != corev1.AppArmorProfileTypeUnconfined {
		t.Errorf("Expected AppArmorProfile type to be Unconfined, got %v",
			result.SecurityContext.AppArmorProfile.Type)
	}
}
