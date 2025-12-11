package nodeconfigurator

import (
	"testing"

	"k8s.io/utils/ptr"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
)

func TestRenderPodSpecHostUsers(t *testing.T) {
	tests := []struct {
		name              string
		nodeConfigurator  slurmv1alpha1.NodeConfiguratorSpec
		expectedHostUsers *bool
	}{
		{
			name: "rebooter enabled with HostUsers nil",
			nodeConfigurator: slurmv1alpha1.NodeConfiguratorSpec{
				Rebooter: slurmv1alpha1.Rebooter{
					Enabled: true,
					PodConfig: slurmv1alpha1.PodConfig{
						HostUsers: nil,
					},
				},
				CustomContainer: slurmv1alpha1.CustomContainer{
					Enabled: false,
				},
			},
			expectedHostUsers: nil,
		},
		{
			name: "rebooter enabled with HostUsers false",
			nodeConfigurator: slurmv1alpha1.NodeConfiguratorSpec{
				Rebooter: slurmv1alpha1.Rebooter{
					Enabled: true,
					PodConfig: slurmv1alpha1.PodConfig{
						HostUsers: ptr.To(false),
					},
				},
				CustomContainer: slurmv1alpha1.CustomContainer{
					Enabled: false,
				},
			},
			expectedHostUsers: ptr.To(false),
		},
		{
			name: "rebooter enabled with HostUsers true",
			nodeConfigurator: slurmv1alpha1.NodeConfiguratorSpec{
				Rebooter: slurmv1alpha1.Rebooter{
					Enabled: true,
					PodConfig: slurmv1alpha1.PodConfig{
						HostUsers: ptr.To(true),
					},
				},
				CustomContainer: slurmv1alpha1.CustomContainer{
					Enabled: false,
				},
			},
			expectedHostUsers: ptr.To(true),
		},
		{
			name: "sleep container enabled with HostUsers nil",
			nodeConfigurator: slurmv1alpha1.NodeConfiguratorSpec{
				Rebooter: slurmv1alpha1.Rebooter{
					Enabled: false,
				},
				CustomContainer: slurmv1alpha1.CustomContainer{
					Enabled: true,
					PodConfig: slurmv1alpha1.PodConfig{
						HostUsers: nil,
					},
				},
			},
			expectedHostUsers: nil,
		},
		{
			name: "sleep container enabled with HostUsers false",
			nodeConfigurator: slurmv1alpha1.NodeConfiguratorSpec{
				Rebooter: slurmv1alpha1.Rebooter{
					Enabled: false,
				},
				CustomContainer: slurmv1alpha1.CustomContainer{
					Enabled: true,
					PodConfig: slurmv1alpha1.PodConfig{
						HostUsers: ptr.To(false),
					},
				},
			},
			expectedHostUsers: ptr.To(false),
		},
		{
			name: "sleep container enabled with HostUsers true",
			nodeConfigurator: slurmv1alpha1.NodeConfiguratorSpec{
				Rebooter: slurmv1alpha1.Rebooter{
					Enabled: false,
				},
				CustomContainer: slurmv1alpha1.CustomContainer{
					Enabled: true,
					PodConfig: slurmv1alpha1.PodConfig{
						HostUsers: ptr.To(true),
					},
				},
			},
			expectedHostUsers: ptr.To(true),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			podSpec := renderPodSpec(tt.nodeConfigurator)

			// Check HostUsers field in PodSpec
			actualHostUsers := podSpec.HostUsers

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

func TestGetHostUsers(t *testing.T) {
	tests := []struct {
		name              string
		nodeConfigurator  slurmv1alpha1.NodeConfiguratorSpec
		expectedHostUsers *bool
	}{
		{
			name: "rebooter enabled returns rebooter HostUsers",
			nodeConfigurator: slurmv1alpha1.NodeConfiguratorSpec{
				Rebooter: slurmv1alpha1.Rebooter{
					Enabled: true,
					PodConfig: slurmv1alpha1.PodConfig{
						HostUsers: ptr.To(true),
					},
				},
				CustomContainer: slurmv1alpha1.CustomContainer{
					Enabled: false,
					PodConfig: slurmv1alpha1.PodConfig{
						HostUsers: ptr.To(false),
					},
				},
			},
			expectedHostUsers: ptr.To(true),
		},
		{
			name: "sleep container enabled returns sleep container HostUsers",
			nodeConfigurator: slurmv1alpha1.NodeConfiguratorSpec{
				Rebooter: slurmv1alpha1.Rebooter{
					Enabled: false,
					PodConfig: slurmv1alpha1.PodConfig{
						HostUsers: ptr.To(true),
					},
				},
				CustomContainer: slurmv1alpha1.CustomContainer{
					Enabled: true,
					PodConfig: slurmv1alpha1.PodConfig{
						HostUsers: ptr.To(false),
					},
				},
			},
			expectedHostUsers: ptr.To(false),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getHostUsers(tt.nodeConfigurator)

			if tt.expectedHostUsers == nil {
				if result != nil {
					t.Errorf("Expected HostUsers to be nil, got %v", *result)
				}
			} else {
				if result == nil {
					t.Errorf("Expected HostUsers to be %v, got nil", *tt.expectedHostUsers)
				} else if *result != *tt.expectedHostUsers {
					t.Errorf("Expected HostUsers to be %v, got %v", *tt.expectedHostUsers, *result)
				}
			}
		})
	}
}
