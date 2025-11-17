package check

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestNewNodeLabelMatcher(t *testing.T) {
	tests := []struct {
		name                        string
		maintenanceIgnoreNodeLabels string
		wantLabels                  map[string][]string
		wantErr                     bool
		errContains                 string
	}{
		{
			name:                        "empty string",
			maintenanceIgnoreNodeLabels: "",
			wantLabels:                  map[string][]string{},
			wantErr:                     false,
		},
		{
			name:                        "single label",
			maintenanceIgnoreNodeLabels: "env=prod",
			wantLabels: map[string][]string{
				"env": {"prod"},
			},
			wantErr: false,
		},
		{
			name:                        "multiple labels",
			maintenanceIgnoreNodeLabels: "env=prod,tier=critical,zone=us-west",
			wantLabels: map[string][]string{
				"env":  []string{"prod"},
				"tier": []string{"critical"},
				"zone": []string{"us-west"},
			},
			wantErr: false,
		},
		{
			name:                        "labels with spaces",
			maintenanceIgnoreNodeLabels: " env = prod , tier = critical ",
			wantLabels: map[string][]string{
				"env":  []string{"prod"},
				"tier": []string{"critical"},
			},
			wantErr: false,
		},
		{
			name:                        "labels with extra commas",
			maintenanceIgnoreNodeLabels: "env=prod,,tier=critical,",
			wantLabels: map[string][]string{
				"env":  []string{"prod"},
				"tier": []string{"critical"},
			},
			wantErr: false,
		},
		{
			name:                        "duplicate keys keep all values",
			maintenanceIgnoreNodeLabels: "env=prod,env=staging",
			wantLabels: map[string][]string{
				"env": []string{"prod", "staging"},
			},
			wantErr: false,
		},
		{
			name:                        "invalid format - missing value",
			maintenanceIgnoreNodeLabels: "env=prod,tier",
			wantErr:                     true,
			errContains:                 "invalid label format",
		},
		{
			name:                        "invalid format - missing key",
			maintenanceIgnoreNodeLabels: "=prod",
			wantErr:                     true,
			errContains:                 "key and value cannot be empty",
		},
		{
			name:                        "invalid format - missing equals",
			maintenanceIgnoreNodeLabels: "envprod",
			wantErr:                     true,
			errContains:                 "invalid label format",
		},
		{
			name:                        "invalid format - empty value",
			maintenanceIgnoreNodeLabels: "env=",
			wantErr:                     true,
			errContains:                 "key and value cannot be empty",
		},
		{
			name:                        "invalid format - empty key",
			maintenanceIgnoreNodeLabels: " =value",
			wantErr:                     true,
			errContains:                 "key and value cannot be empty",
		},
		{
			name:                        "complex label keys",
			maintenanceIgnoreNodeLabels: "topology.kubernetes.io/zone=us-west-1a,node.kubernetes.io/instance-type=m5.large",
			wantLabels: map[string][]string{
				"topology.kubernetes.io/zone":      []string{"us-west-1a"},
				"node.kubernetes.io/instance-type": []string{"m5.large"},
			},
			wantErr: false,
		},
		{
			name:                        "values with equals sign",
			maintenanceIgnoreNodeLabels: "annotation=key=value",
			wantLabels: map[string][]string{
				"annotation": []string{"key=value"},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matcher, err := NewNodeLabelMatcher(tt.maintenanceIgnoreNodeLabels)

			if tt.wantErr {
				require.Error(t, err)
				if tt.errContains != "" {
					assert.Contains(t, err.Error(), tt.errContains)
				}
				return
			}

			require.NoError(t, err)
			require.NotNil(t, matcher)
			assert.Equal(t, tt.wantLabels, matcher.GetIgnoredLabels())
		})
	}
}

func TestNodeLabelMatcher_ShouldIgnoreNode(t *testing.T) {
	tests := []struct {
		name                        string
		maintenanceIgnoreNodeLabels string
		nodeLabels                  map[string]string
		want                        bool
	}{
		{
			name:                        "no ignored labels configured",
			maintenanceIgnoreNodeLabels: "",
			nodeLabels: map[string]string{
				"env": "prod",
			},
			want: false,
		},
		{
			name:                        "node has matching label",
			maintenanceIgnoreNodeLabels: "env=prod",
			nodeLabels: map[string]string{
				"env": "prod",
			},
			want: true,
		},
		{
			name:                        "node has non-matching label value",
			maintenanceIgnoreNodeLabels: "env=prod",
			nodeLabels: map[string]string{
				"env": "dev",
			},
			want: false,
		},
		{
			name:                        "node missing ignored label key",
			maintenanceIgnoreNodeLabels: "env=prod",
			nodeLabels: map[string]string{
				"tier": "critical",
			},
			want: false,
		},
		{
			name:                        "node has one of multiple ignored labels",
			maintenanceIgnoreNodeLabels: "env=prod,tier=critical",
			nodeLabels: map[string]string{
				"env":  "dev",
				"tier": "critical",
			},
			want: true,
		},
		{
			name:                        "multiple ignored labels - all match",
			maintenanceIgnoreNodeLabels: "env=prod,tier=critical",
			nodeLabels: map[string]string{
				"env":  "prod",
				"tier": "critical",
			},
			want: true,
		},
		{
			name:                        "node matches alternate value for same key",
			maintenanceIgnoreNodeLabels: "env=prod,env=staging",
			nodeLabels: map[string]string{
				"env": "staging",
			},
			want: true,
		},
		{
			name:                        "node value not in ignored values for key",
			maintenanceIgnoreNodeLabels: "env=prod,env=staging",
			nodeLabels: map[string]string{
				"env": "dev",
			},
			want: false,
		},
		{
			name:                        "node has extra labels but matches one ignored",
			maintenanceIgnoreNodeLabels: "env=prod",
			nodeLabels: map[string]string{
				"env":  "prod",
				"tier": "standard",
				"zone": "us-west",
				"app":  "backend",
			},
			want: true,
		},
		{
			name:                        "node has no labels",
			maintenanceIgnoreNodeLabels: "env=prod",
			nodeLabels:                  map[string]string{},
			want:                        false,
		},
		{
			name:                        "complex label keys match",
			maintenanceIgnoreNodeLabels: "topology.kubernetes.io/zone=us-west-1a",
			nodeLabels: map[string]string{
				"topology.kubernetes.io/zone": "us-west-1a",
			},
			want: true,
		},
		{
			name:                        "complex label keys don't match",
			maintenanceIgnoreNodeLabels: "topology.kubernetes.io/zone=us-west-1a",
			nodeLabels: map[string]string{
				"topology.kubernetes.io/zone": "us-west-1b",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matcher, err := NewNodeLabelMatcher(tt.maintenanceIgnoreNodeLabels)
			require.NoError(t, err)

			node := &corev1.Node{
				ObjectMeta: metav1.ObjectMeta{
					Name:   "test-node",
					Labels: tt.nodeLabels,
				},
			}

			got := matcher.ShouldIgnoreNode(node)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNodeLabelMatcher_ShouldIgnoreNode_NilNode(t *testing.T) {
	matcher, err := NewNodeLabelMatcher("env=prod")
	require.NoError(t, err)

	got := matcher.ShouldIgnoreNode(nil)
	assert.False(t, got, "should return false for nil node")
}

func TestNodeLabelMatcher_HasIgnoredLabels(t *testing.T) {
	tests := []struct {
		name                        string
		maintenanceIgnoreNodeLabels string
		want                        bool
	}{
		{
			name:                        "no ignored labels",
			maintenanceIgnoreNodeLabels: "",
			want:                        false,
		},
		{
			name:                        "has ignored labels",
			maintenanceIgnoreNodeLabels: "env=prod",
			want:                        true,
		},
		{
			name:                        "multiple ignored labels",
			maintenanceIgnoreNodeLabels: "env=prod,tier=critical",
			want:                        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matcher, err := NewNodeLabelMatcher(tt.maintenanceIgnoreNodeLabels)
			require.NoError(t, err)

			got := matcher.HasIgnoredLabels()
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestNodeLabelMatcher_GetIgnoredLabels(t *testing.T) {
	t.Run("returns copy of labels", func(t *testing.T) {
		matcher, err := NewNodeLabelMatcher("env=prod,tier=critical")
		require.NoError(t, err)

		labels1 := matcher.GetIgnoredLabels()
		labels2 := matcher.GetIgnoredLabels()

		// Should be equal
		assert.Equal(t, labels1, labels2)

		// Should be different instances (copy not reference)
		labels1["new"] = []string{"label"}
		assert.NotEqual(t, labels1, labels2)
		assert.NotContains(t, matcher.GetIgnoredLabels(), "new")

		labels1["env"][0] = "modified"
		assert.Equal(t, "prod", matcher.GetIgnoredLabels()["env"][0])
	})

	t.Run("empty matcher returns empty map", func(t *testing.T) {
		matcher, err := NewNodeLabelMatcher("")
		require.NoError(t, err)

		labels := matcher.GetIgnoredLabels()
		assert.NotNil(t, labels)
		assert.Empty(t, labels)
	})
}
