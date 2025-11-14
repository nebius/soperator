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
		name              string
		ignoredNodeLabels string
		wantLabels        map[string]string
		wantErr           bool
		errContains       string
	}{
		{
			name:              "empty string",
			ignoredNodeLabels: "",
			wantLabels:        map[string]string{},
			wantErr:           false,
		},
		{
			name:              "single label",
			ignoredNodeLabels: "env=prod",
			wantLabels: map[string]string{
				"env": "prod",
			},
			wantErr: false,
		},
		{
			name:              "multiple labels",
			ignoredNodeLabels: "env=prod,tier=critical,zone=us-west",
			wantLabels: map[string]string{
				"env":  "prod",
				"tier": "critical",
				"zone": "us-west",
			},
			wantErr: false,
		},
		{
			name:              "labels with spaces",
			ignoredNodeLabels: " env = prod , tier = critical ",
			wantLabels: map[string]string{
				"env":  "prod",
				"tier": "critical",
			},
			wantErr: false,
		},
		{
			name:              "labels with extra commas",
			ignoredNodeLabels: "env=prod,,tier=critical,",
			wantLabels: map[string]string{
				"env":  "prod",
				"tier": "critical",
			},
			wantErr: false,
		},
		{
			name:              "invalid format - missing value",
			ignoredNodeLabels: "env=prod,tier",
			wantErr:           true,
			errContains:       "invalid label format",
		},
		{
			name:              "invalid format - missing key",
			ignoredNodeLabels: "=prod",
			wantErr:           true,
			errContains:       "key and value cannot be empty",
		},
		{
			name:              "invalid format - missing equals",
			ignoredNodeLabels: "envprod",
			wantErr:           true,
			errContains:       "invalid label format",
		},
		{
			name:              "invalid format - empty value",
			ignoredNodeLabels: "env=",
			wantErr:           true,
			errContains:       "key and value cannot be empty",
		},
		{
			name:              "invalid format - empty key",
			ignoredNodeLabels: " =value",
			wantErr:           true,
			errContains:       "key and value cannot be empty",
		},
		{
			name:              "complex label keys",
			ignoredNodeLabels: "topology.kubernetes.io/zone=us-west-1a,node.kubernetes.io/instance-type=m5.large",
			wantLabels: map[string]string{
				"topology.kubernetes.io/zone":      "us-west-1a",
				"node.kubernetes.io/instance-type": "m5.large",
			},
			wantErr: false,
		},
		{
			name:              "values with equals sign",
			ignoredNodeLabels: "annotation=key=value",
			wantLabels: map[string]string{
				"annotation": "key=value",
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matcher, err := NewNodeLabelMatcher(tt.ignoredNodeLabels)

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
		name              string
		ignoredNodeLabels string
		nodeLabels        map[string]string
		want              bool
	}{
		{
			name:              "no ignored labels configured",
			ignoredNodeLabels: "",
			nodeLabels: map[string]string{
				"env": "prod",
			},
			want: false,
		},
		{
			name:              "node has matching label",
			ignoredNodeLabels: "env=prod",
			nodeLabels: map[string]string{
				"env": "prod",
			},
			want: true,
		},
		{
			name:              "node has non-matching label value",
			ignoredNodeLabels: "env=prod",
			nodeLabels: map[string]string{
				"env": "dev",
			},
			want: false,
		},
		{
			name:              "node missing ignored label key",
			ignoredNodeLabels: "env=prod",
			nodeLabels: map[string]string{
				"tier": "critical",
			},
			want: false,
		},
		{
			name:              "node has one of multiple ignored labels",
			ignoredNodeLabels: "env=prod,tier=critical",
			nodeLabels: map[string]string{
				"env":  "dev",
				"tier": "critical",
			},
			want: true,
		},
		{
			name:              "node has all ignored labels",
			ignoredNodeLabels: "env=prod,tier=critical",
			nodeLabels: map[string]string{
				"env":  "prod",
				"tier": "critical",
			},
			want: true,
		},
		{
			name:              "node has extra labels but matches one ignored",
			ignoredNodeLabels: "env=prod",
			nodeLabels: map[string]string{
				"env":  "prod",
				"tier": "standard",
				"zone": "us-west",
				"app":  "backend",
			},
			want: true,
		},
		{
			name:              "node has no labels",
			ignoredNodeLabels: "env=prod",
			nodeLabels:        map[string]string{},
			want:              false,
		},
		{
			name:              "complex label keys match",
			ignoredNodeLabels: "topology.kubernetes.io/zone=us-west-1a",
			nodeLabels: map[string]string{
				"topology.kubernetes.io/zone": "us-west-1a",
			},
			want: true,
		},
		{
			name:              "complex label keys don't match",
			ignoredNodeLabels: "topology.kubernetes.io/zone=us-west-1a",
			nodeLabels: map[string]string{
				"topology.kubernetes.io/zone": "us-west-1b",
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matcher, err := NewNodeLabelMatcher(tt.ignoredNodeLabels)
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
		name              string
		ignoredNodeLabels string
		want              bool
	}{
		{
			name:              "no ignored labels",
			ignoredNodeLabels: "",
			want:              false,
		},
		{
			name:              "has ignored labels",
			ignoredNodeLabels: "env=prod",
			want:              true,
		},
		{
			name:              "multiple ignored labels",
			ignoredNodeLabels: "env=prod,tier=critical",
			want:              true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			matcher, err := NewNodeLabelMatcher(tt.ignoredNodeLabels)
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
		labels1["new"] = "label"
		assert.NotEqual(t, labels1, labels2)
		assert.NotContains(t, matcher.GetIgnoredLabels(), "new")
	})

	t.Run("empty matcher returns empty map", func(t *testing.T) {
		matcher, err := NewNodeLabelMatcher("")
		require.NoError(t, err)

		labels := matcher.GetIgnoredLabels()
		assert.NotNil(t, labels)
		assert.Empty(t, labels)
	})
}
