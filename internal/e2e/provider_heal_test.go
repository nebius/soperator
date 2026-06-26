package e2e

import (
	"testing"

	tfjson "github.com/hashicorp/terraform-json"
)

const (
	nebiusPublicFQN = "registry.terraform.io/nebius/nebius"
	nebiusMirrorFQN = "terraform-provider.storage.eu-north1.nebius.cloud/nebius/nebius"
)

func TestIsNebiusProviderFQN(t *testing.T) {
	cases := map[string]bool{
		nebiusPublicFQN:                        true,
		nebiusMirrorFQN:                        true,
		"registry.terraform.io/hashicorp/helm": false,
		"registry.terraform.io/nebius/nebius2": false,
		"":                                     false,
	}
	for fqn, want := range cases {
		if got := isNebiusProviderFQN(fqn); got != want {
			t.Errorf("isNebiusProviderFQN(%q) = %v, want %v", fqn, got, want)
		}
	}
}

func TestPickTargetNebiusFQN(t *testing.T) {
	tests := []struct {
		name     string
		config   []string
		stateFQN string
		want     string
	}{
		{"mirror state, public config", []string{nebiusPublicFQN}, nebiusMirrorFQN, nebiusPublicFQN},
		{"public state, mirror config", []string{nebiusMirrorFQN}, nebiusPublicFQN, nebiusMirrorFQN},
		{"already aligned", []string{nebiusPublicFQN}, nebiusPublicFQN, nebiusPublicFQN},
		{"both present, mirror state", []string{nebiusMirrorFQN, nebiusPublicFQN}, nebiusMirrorFQN, nebiusPublicFQN},
		{"both present, public state", []string{nebiusPublicFQN, nebiusMirrorFQN}, nebiusPublicFQN, nebiusMirrorFQN},
		{"no nebius in config", nil, nebiusMirrorFQN, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := pickTargetNebiusFQN(tc.config, tc.stateFQN); got != tc.want {
				t.Errorf("pickTargetNebiusFQN(%v, %q) = %q, want %q", tc.config, tc.stateFQN, got, tc.want)
			}
		})
	}
}

func TestFirstNebiusProviderInState(t *testing.T) {
	nodeGroupInChild := &tfjson.State{
		Values: &tfjson.StateValues{
			RootModule: &tfjson.StateModule{
				Resources: []*tfjson.StateResource{
					{Address: "data.something.this", ProviderName: "registry.terraform.io/hashicorp/null"},
				},
				ChildModules: []*tfjson.StateModule{
					{
						Address: "module.k8s",
						Resources: []*tfjson.StateResource{
							{Address: "module.k8s.nebius_mk8s_v1_node_group.worker", ProviderName: nebiusMirrorFQN},
						},
					},
				},
			},
		},
	}
	noNebius := &tfjson.State{
		Values: &tfjson.StateValues{
			RootModule: &tfjson.StateModule{
				Resources: []*tfjson.StateResource{
					{Address: "helm_release.x", ProviderName: "registry.terraform.io/hashicorp/helm"},
				},
			},
		},
	}

	tests := []struct {
		name  string
		state *tfjson.State
		want  string
	}{
		{"nebius node group nested in child module", nodeGroupInChild, nebiusMirrorFQN},
		{"no nebius resources", noNebius, ""},
		{"nil values", &tfjson.State{}, ""},
		{"nil state", nil, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := firstNebiusProviderInState(tc.state); got != tc.want {
				t.Errorf("firstNebiusProviderInState() = %q, want %q", got, tc.want)
			}
		})
	}
}
