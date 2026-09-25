package common

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/values"
)

func TestResolveNodeSetRefs(t *testing.T) {
	tests := []struct {
		name     string
		cluster  *values.SlurmCluster
		expected NodeSetRefResolution
	}{
		{
			name: "All refs resolvable",
			cluster: &values.SlurmCluster{
				NodeSets: []slurmv1alpha1.NodeSet{
					nodeSetWithReplicas("nodeA", 1),
					nodeSetWithReplicas("nodeB", 4),
				},
				PartitionConfiguration: values.PartitionConfiguration{
					ConfigType: slurmv1.PartitionConfigTypeStructured,
					Partitions: []slurmv1.Partition{
						{Name: "gpu", NodeSetRefs: []string{"nodeA", "nodeB"}},
						{Name: "main", IsAll: true},
					},
				},
			},
			expected: NodeSetRefResolution{},
		},
		{
			name: "Missing and scaled down nodesets",
			cluster: &values.SlurmCluster{
				NodeSets: []slurmv1alpha1.NodeSet{
					nodeSetWithReplicas("nodeA", 1),
					nodeSetWithReplicas("scaled-down", 0),
				},
				PartitionConfiguration: values.PartitionConfiguration{
					ConfigType: slurmv1.PartitionConfigTypeStructured,
					Partitions: []slurmv1.Partition{
						{Name: "gpu", NodeSetRefs: []string{"nodeA", "missing", "scaled-down"}},
					},
				},
			},
			expected: NodeSetRefResolution{
				Ignored: []IgnoredNodeSetRef{
					{Partition: "gpu", NodeSet: "missing", Reason: NodeSetRefIgnoreReasonNotFound},
					{Partition: "gpu", NodeSet: "scaled-down", Reason: NodeSetRefIgnoreReasonNoReplicas},
				},
			},
		},
		{
			name: "Partition losing all refs is reported as left without nodes",
			cluster: &values.SlurmCluster{
				PartitionConfiguration: values.PartitionConfiguration{
					ConfigType: slurmv1.PartitionConfigTypeStructured,
					Partitions: []slurmv1.Partition{
						{Name: "gpu", NodeSetRefs: []string{"missing"}},
					},
				},
			},
			expected: NodeSetRefResolution{
				Ignored: []IgnoredNodeSetRef{
					{Partition: "gpu", NodeSet: "missing", Reason: NodeSetRefIgnoreReasonNotFound},
				},
				PartitionsWithoutNodes: []string{"gpu"},
			},
		},
		{
			name: "Negative replicas are treated as no replicas",
			cluster: &values.SlurmCluster{
				NodeSets: []slurmv1alpha1.NodeSet{
					nodeSetWithReplicas("nodeA", -1),
				},
				PartitionConfiguration: values.PartitionConfiguration{
					ConfigType: slurmv1.PartitionConfigTypeStructured,
					Partitions: []slurmv1.Partition{
						{Name: "gpu", NodeSetRefs: []string{"nodeA"}},
					},
				},
			},
			expected: NodeSetRefResolution{
				Ignored: []IgnoredNodeSetRef{
					{Partition: "gpu", NodeSet: "nodeA", Reason: NodeSetRefIgnoreReasonNoReplicas},
				},
				PartitionsWithoutNodes: []string{"gpu"},
			},
		},
		{
			name: "Non-structured config type keeps its leftover partitions unreported",
			cluster: &values.SlurmCluster{
				PartitionConfiguration: values.PartitionConfiguration{
					ConfigType: slurmv1.PartitionConfigTypeDefault,
					Partitions: []slurmv1.Partition{
						{Name: "gpu", NodeSetRefs: []string{"missing"}},
					},
				},
			},
			expected: NodeSetRefResolution{},
		},
		{
			name: "Partitions without refs are not reported",
			cluster: &values.SlurmCluster{
				PartitionConfiguration: values.PartitionConfiguration{
					ConfigType: slurmv1.PartitionConfigTypeStructured,
					Partitions: []slurmv1.Partition{
						{Name: "main", IsAll: true},
						{Name: "invalid"},
					},
				},
			},
			expected: NodeSetRefResolution{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resolution := ResolveNodeSetRefs(tt.cluster)

			assert.Equal(t, tt.expected, resolution)
			assert.Equal(t, tt.expected.IsEmpty(), resolution.IsEmpty())
		})
	}
}

func TestFormatIgnoredNodeSetRefs(t *testing.T) {
	t.Run("lists refs and partitions left without nodes", func(t *testing.T) {
		message := FormatIgnoredNodeSetRefs(NodeSetRefResolution{
			Ignored: []IgnoredNodeSetRef{
				{Partition: "gpu", NodeSet: "missing", Reason: NodeSetRefIgnoreReasonNotFound},
				{Partition: "cpu", NodeSet: "scaled-down", Reason: NodeSetRefIgnoreReasonNoReplicas},
			},
			PartitionsWithoutNodes: []string{"gpu"},
		}, 32768)

		assert.Equal(
			t,
			`partition "gpu": nodeSetRef "missing" ignored (NodeSet does not exist); `+
				`partition "cpu": nodeSetRef "scaled-down" ignored (NodeSet has no replicas); `+
				`partition "gpu" has no nodes: none of its nodeSetRefs are usable`,
			message,
		)
	})

	t.Run("empty resolution renders nothing", func(t *testing.T) {
		assert.Empty(t, FormatIgnoredNodeSetRefs(NodeSetRefResolution{}, 32768))
	})

	t.Run("truncates the tail that does not fit", func(t *testing.T) {
		var ignored []IgnoredNodeSetRef
		for i := range 100 {
			ignored = append(ignored, IgnoredNodeSetRef{
				Partition: "gpu",
				NodeSet:   fmt.Sprintf("missing-%d", i),
				Reason:    NodeSetRefIgnoreReasonNotFound,
			})
		}

		const limit = 256
		message := FormatIgnoredNodeSetRefs(NodeSetRefResolution{Ignored: ignored}, limit)

		assert.LessOrEqual(t, len(message), limit)
		assert.Contains(t, message, `partition "gpu": nodeSetRef "missing-0" ignored`)
		assert.NotContains(t, message, `"missing-99"`)
		assert.True(t, strings.HasSuffix(message, " more"), "message must end with the truncation tail: %s", message)
	})
}
