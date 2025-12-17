package common_test

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/utils/ptr"

	"nebius.ai/slurm-operator/internal/render/common"
)

func TestParseAppArmorProfile(t *testing.T) {
	tests := []struct {
		name        string
		profileStr  string
		expected    *corev1.AppArmorProfile
		description string
	}{
		{
			name:        "empty string",
			profileStr:  "",
			expected:    nil,
			description: "should return nil for empty string",
		},
		{
			name:       "unconfined profile",
			profileStr: "unconfined",
			expected: &corev1.AppArmorProfile{
				Type: corev1.AppArmorProfileTypeUnconfined,
			},
			description: "should return Unconfined type",
		},
		{
			name:       "localhost with profile name",
			profileStr: "localhost/my-profile",
			expected: &corev1.AppArmorProfile{
				Type:             corev1.AppArmorProfileTypeLocalhost,
				LocalhostProfile: ptr.To("my-profile"),
			},
			description: "should parse localhost/profile-name format",
		},
		{
			name:       "profile name without localhost prefix",
			profileStr: "my-profile",
			expected: &corev1.AppArmorProfile{
				Type:             corev1.AppArmorProfileTypeLocalhost,
				LocalhostProfile: ptr.To("my-profile"),
			},
			description: "should default to localhost type for plain profile names",
		},
		{
			name:       "complex profile name",
			profileStr: "localhost/slurm-cluster-default",
			expected: &corev1.AppArmorProfile{
				Type:             corev1.AppArmorProfileTypeLocalhost,
				LocalhostProfile: ptr.To("slurm-cluster-default"),
			},
			description: "should handle complex profile names",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := common.ParseAppArmorProfile(tt.profileStr)

			if tt.expected == nil {
				if result != nil {
					t.Errorf("ParseAppArmorProfile(%q) = %v, want nil", tt.profileStr, result)
				}
				return
			}

			if result == nil {
				t.Errorf("ParseAppArmorProfile(%q) = nil, want %v", tt.profileStr, tt.expected)
				return
			}

			if result.Type != tt.expected.Type {
				t.Errorf("ParseAppArmorProfile(%q).Type = %v, want %v", tt.profileStr, result.Type, tt.expected.Type)
			}

			if tt.expected.LocalhostProfile != nil {
				if result.LocalhostProfile == nil {
					t.Errorf("ParseAppArmorProfile(%q).LocalhostProfile = nil, want %v", tt.profileStr, *tt.expected.LocalhostProfile)
				} else if *result.LocalhostProfile != *tt.expected.LocalhostProfile {
					t.Errorf("ParseAppArmorProfile(%q).LocalhostProfile = %v, want %v", tt.profileStr, *result.LocalhostProfile, *tt.expected.LocalhostProfile)
				}
			} else if result.LocalhostProfile != nil {
				t.Errorf("ParseAppArmorProfile(%q).LocalhostProfile = %v, want nil", tt.profileStr, *result.LocalhostProfile)
			}
		})
	}
}
