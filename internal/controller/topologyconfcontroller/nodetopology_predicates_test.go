package topologyconfcontroller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const testTierPrefix = "topology.nebius.com"

func nodeWithLabels(labels map[string]string) *corev1.Node {
	return &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: "node-a", Labels: labels}}
}

// TestNodeTierPredicates pins that the controller wakes for any tier label, not just tier-1.
//
// Block topologies are built from tier-0 alone, so watching tier-1 left every tier-0 change
// undelivered: the ResourceDistribution kept a stale snapshot, and the blocks stayed in the
// catch-all "unknown" block no matter how the nodes were labelled.
func TestNodeTierPredicates(t *testing.T) {
	r := &NodeTopologyReconciler{topologyLabelPrefix: testTierPrefix}

	t.Run("carries", func(t *testing.T) {
		t.Run("a node with only tier-0 is interesting", func(t *testing.T) {
			assert.True(t, r.NodeCarriesTierLabels(nodeWithLabels(map[string]string{
				testTierPrefix + "/tier-0": "block-0",
			})))
		})

		t.Run("a node with only tier-1 is interesting", func(t *testing.T) {
			assert.True(t, r.NodeCarriesTierLabels(nodeWithLabels(map[string]string{
				testTierPrefix + "/tier-1": "leaf01",
			})))
		})

		t.Run("a node with no tier labels is not", func(t *testing.T) {
			assert.False(t, r.NodeCarriesTierLabels(nodeWithLabels(map[string]string{
				"kubernetes.io/hostname": "node-a",
			})))
		})

		t.Run("a label of another prefix does not count", func(t *testing.T) {
			assert.False(t, r.NodeCarriesTierLabels(nodeWithLabels(map[string]string{
				"other.example.com/tier-0": "block-0",
			})))
		})
	})

	t.Run("changed", func(t *testing.T) {
		withTiers := map[string]string{
			testTierPrefix + "/tier-1": "leaf01",
			testTierPrefix + "/tier-2": "spine01",
		}

		t.Run("adding tier-0 to a labelled node is a change", func(t *testing.T) {
			after := map[string]string{
				testTierPrefix + "/tier-0": "block-0",
				testTierPrefix + "/tier-1": "leaf01",
				testTierPrefix + "/tier-2": "spine01",
			}

			assert.True(t, r.NodeTierLabelsChanged(nodeWithLabels(withTiers), nodeWithLabels(after)),
				"this is the case a tier-1-only predicate dropped")
		})

		t.Run("changing tier-0 is a change", func(t *testing.T) {
			before := map[string]string{testTierPrefix + "/tier-0": "block-0"}
			after := map[string]string{testTierPrefix + "/tier-0": "block-1"}

			assert.True(t, r.NodeTierLabelsChanged(nodeWithLabels(before), nodeWithLabels(after)))
		})

		t.Run("removing tier-0 is a change", func(t *testing.T) {
			before := map[string]string{
				testTierPrefix + "/tier-0": "block-0",
				testTierPrefix + "/tier-1": "leaf01",
			}
			after := map[string]string{testTierPrefix + "/tier-1": "leaf01"}

			assert.True(t, r.NodeTierLabelsChanged(nodeWithLabels(before), nodeWithLabels(after)))
		})

		t.Run("an unrelated label does not wake the controller", func(t *testing.T) {
			after := map[string]string{
				testTierPrefix + "/tier-1": "leaf01",
				testTierPrefix + "/tier-2": "spine01",
				"nebius.com/gpu":           "true",
			}

			assert.False(t, r.NodeTierLabelsChanged(nodeWithLabels(withTiers), nodeWithLabels(after)))
		})

		t.Run("an identical node is not a change", func(t *testing.T) {
			assert.False(t, r.NodeTierLabelsChanged(nodeWithLabels(withTiers), nodeWithLabels(withTiers)))
		})
	})
}
