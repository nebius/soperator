package e2e

import (
	"slices"
	"testing"
)

func TestAddressesToPrune(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "prunes helm_release and o11y static key, keeps the rest",
			in: []string{
				"module.slurm.helm_release.soperator_fluxcd_cm",
				"module.slurm.helm_release.soperator_fluxcd_bootstrap",
				"module.o11y.terraform_data.o11y_static_key_secret",
				"module.o11y.terraform_data.opentelemetry_collector_cm",
				"module.k8s.nebius_mk8s_v1_node_group.worker_v2",
				"data.nebius_iam_v1_project.this",
			},
			want: []string{
				"module.slurm.helm_release.soperator_fluxcd_cm",
				"module.slurm.helm_release.soperator_fluxcd_bootstrap",
				"module.o11y.terraform_data.o11y_static_key_secret",
			},
		},
		{
			name: "nothing to prune",
			in:   []string{"module.k8s.nebius_mk8s_v1_node_group.worker_v2", "module.o11y.terraform_data.opentelemetry_collector_cm"},
			want: nil,
		},
		{
			name: "empty input",
			in:   nil,
			want: nil,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := addressesToPrune(tc.in); !slices.Equal(got, tc.want) {
				t.Errorf("addressesToPrune() = %v, want %v", got, tc.want)
			}
		})
	}
}
