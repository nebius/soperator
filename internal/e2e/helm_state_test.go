package e2e

import (
	"slices"
	"testing"
)

func TestHelmReleaseAddresses(t *testing.T) {
	tests := []struct {
		name      string
		rawState  string
		want      []string
		wantError bool
	}{
		{
			name: "module-scoped and root helm releases, non-helm ignored",
			rawState: `{
				"version": 4,
				"resources": [
					{"module": "module.slurm", "mode": "managed", "type": "helm_release", "name": "soperator_fluxcd_cm"},
					{"module": "module.slurm", "mode": "managed", "type": "helm_release", "name": "soperator_fluxcd_ad_hoc_cm"},
					{"module": "module.k8s", "mode": "managed", "type": "nebius_mk8s_v1_node_group", "name": "worker_v2"},
					{"mode": "managed", "type": "helm_release", "name": "root_release"},
					{"mode": "data", "type": "nebius_iam_v1_project", "name": "this"}
				]
			}`,
			want: []string{
				"module.slurm.helm_release.soperator_fluxcd_cm",
				"module.slurm.helm_release.soperator_fluxcd_ad_hoc_cm",
				"helm_release.root_release",
			},
		},
		{
			name:     "no helm releases",
			rawState: `{"version": 4, "resources": [{"module": "module.k8s", "mode": "managed", "type": "nebius_mk8s_v1_node_group", "name": "worker_v2"}]}`,
			want:     nil,
		},
		{
			name:     "empty state",
			rawState: `{"version": 4, "resources": []}`,
			want:     nil,
		},
		{
			name:      "malformed json",
			rawState:  `{"version": 4, "resources": [`,
			wantError: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := helmReleaseAddresses(tc.rawState)
			if tc.wantError {
				if err == nil {
					t.Fatalf("helmReleaseAddresses() expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("helmReleaseAddresses() unexpected error: %v", err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("helmReleaseAddresses() = %v, want %v", got, tc.want)
			}
		})
	}
}
