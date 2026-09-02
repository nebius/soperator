package topologyconfcontroller

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/events"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
)

const treeBefore = `- topology: tree-ib
  cluster_default: true
  tree:
    switches:
      - switch: root
        children: leaf-[0-1]
      - switch: leaf-0
        nodes: worker-[0-3]
      - switch: leaf-1
        nodes: worker-[4-7]
`

func TestTopologyMembership(t *testing.T) {
	membership, err := topologyMembership(treeBefore)
	require.NoError(t, err)

	assert.Equal(t, map[string][]string{
		"tree-ib": {
			"worker-0", "worker-1", "worker-2", "worker-3",
			"worker-4", "worker-5", "worker-6", "worker-7",
		},
	}, membership)
}

// A flat topology names no node, so it must not be mistaken for one that lost all of them.
func TestTopologyMembershipFlatCoversNoNode(t *testing.T) {
	membership, err := topologyMembership("- topology: flat\n  cluster_default: false\n  flat: true\n")
	require.NoError(t, err)

	assert.Equal(t, map[string][]string{"flat": nil}, membership)
}

func TestSummarizeTopologyChange(t *testing.T) {
	tests := []struct {
		name            string
		before, after   string
		expectedSummary string
		expectedDetail  string
	}{
		{
			name:            "nodes moved to another topology",
			before:          treeBefore,
			after:           strings.Replace(treeBefore, "nodes: worker-[4-7]", "nodes: worker-[4-5]", 1) + blockEntry,
			expectedSummary: "+block-nvl72 (2 nodes); tree-ib 6 nodes (+0 -2)",
			expectedDetail:  "+block-nvl72=worker-[6-7]; tree-ib +[] -[worker-[6-7]]",
		},
		{
			name:            "topology added",
			before:          treeBefore,
			after:           treeBefore + blockEntry,
			expectedSummary: "+block-nvl72 (2 nodes)",
			expectedDetail:  "+block-nvl72=worker-[6-7]",
		},
		{
			name:            "topology removed",
			before:          treeBefore + blockEntry,
			after:           treeBefore,
			expectedSummary: "-block-nvl72",
			expectedDetail:  "-block-nvl72=worker-[6-7]",
		},
		{
			name:            "membership untouched",
			before:          treeBefore,
			after:           strings.Replace(treeBefore, "cluster_default: true", "cluster_default: false", 1),
			expectedSummary: "node membership unchanged",
			expectedDetail:  "",
		},
		{
			name:            "previous content is not topology.yaml",
			before:          "not: [a, yaml, list",
			after:           treeBefore,
			expectedSummary: "previous topology.yaml could not be parsed",
			expectedDetail:  "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			change := summarizeTopologyChange(tt.before, tt.after)
			assert.Equal(t, tt.expectedSummary, change.Summary)
			assert.Equal(t, tt.expectedDetail, change.Detail)
		})
	}
}

const blockEntry = `- topology: block-nvl72
  cluster_default: false
  block:
    block_sizes: [2]
    blocks:
      - block: block-0
        nodes: worker-[6-7]
`

func TestSummarizeTopologyChangeTruncatesForTheEventNote(t *testing.T) {
	var before, after strings.Builder
	for i := range 200 {
		before.WriteString(renderTinyTree(i, "worker-0"))
		after.WriteString(renderTinyTree(i, "worker-1"))
	}

	change := summarizeTopologyChange(before.String(), after.String())

	assert.LessOrEqual(t, len(change.Summary), maxTopologyChangeSummary+64)
	assert.Contains(t, change.Summary, "see the operator log for the rest")
	assert.Greater(t, len(change.Detail), len(change.Summary))
}

func renderTinyTree(index int, node string) string {
	return strings.NewReplacer("INDEX", string(rune('a'+index%26))+string(rune('a'+index/26)), "NODE", node).Replace(
		"- topology: tree-INDEX\n  cluster_default: false\n  tree:\n    switches:\n      - switch: leaf-INDEX\n        nodes: NODE\n",
	)
}

func TestRecordTopologyRenderedGoesToTheCluster(t *testing.T) {
	recorder := events.NewFakeRecorder(10)
	reconciler := &WorkerTopologyReconciler{recorder: recorder}
	cluster := &slurmv1.SlurmCluster{
		ObjectMeta: metav1.ObjectMeta{Name: "test-cluster", Namespace: "test-namespace"},
	}

	reconciler.recordTopologyRendered(cluster, summarizeTopologyChange(treeBefore, treeBefore+blockEntry))

	captured := drainEvents(recorder)
	require.Len(t, captured, 1)
	assert.Contains(t, captured[0], reasonTopologyRendered)
	assert.Contains(t, captured[0], "+block-nvl72 (2 nodes)")
}

func TestRecordTopologyRenderedWithoutARecorder(t *testing.T) {
	reconciler := &WorkerTopologyReconciler{}

	assert.NotPanics(t, func() {
		reconciler.recordTopologyRendered(&slurmv1.SlurmCluster{}, topologyChange{})
	})
}
