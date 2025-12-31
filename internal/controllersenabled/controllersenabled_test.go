package controllersenabled

import "testing"

func TestNew(t *testing.T) {
	available := []string{"cluster", "nodeconfigurator", "nodeset", "topology"}

	tests := []struct {
		name      string
		input     string
		want      map[string]bool
		expectErr bool
	}{
		{
			name:  "empty enables all",
			input: "",
			want: map[string]bool{
				"cluster":          true,
				"nodeconfigurator": true,
				"nodeset":          true,
				"topology":         true,
			},
		},
		{
			name:  "explicit enable list",
			input: "cluster,topology",
			want: map[string]bool{
				"cluster":          true,
				"nodeconfigurator": false,
				"nodeset":          false,
				"topology":         true,
			},
		},
		{
			name:  "wildcard enables all",
			input: "*",
			want: map[string]bool{
				"cluster":          true,
				"nodeconfigurator": true,
				"nodeset":          true,
				"topology":         true,
			},
		},
		{
			name:  "disable one from wildcard",
			input: "*, -nodeconfigurator",
			want: map[string]bool{
				"cluster":          true,
				"nodeconfigurator": false,
				"nodeset":          true,
				"topology":         true,
			},
		},
		{
			name:  "disable without wildcard",
			input: "-cluster, topology",
			want: map[string]bool{
				"cluster":          false,
				"nodeconfigurator": false,
				"nodeset":          false,
				"topology":         true,
			},
		},
		{
			name:      "unknown controller",
			input:     "cluster,unknown",
			expectErr: true,
		},
		{
			name:  "case insensitive",
			input: "Cluster,TOPOLOGY",
			want: map[string]bool{
				"cluster":          true,
				"nodeconfigurator": false,
				"nodeset":          false,
				"topology":         true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			set, err := New(tt.input, available)
			if tt.expectErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			for name, wantEnabled := range tt.want {
				if got := set.Enabled(name); got != wantEnabled {
					t.Fatalf("unexpected enabled state for %q: got=%v want=%v", name, got, wantEnabled)
				}
			}
		})
	}
}
