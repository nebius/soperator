package topologyconfcontroller

import (
	"testing"

	api "github.com/SlinkyProject/slurm-client/api/v0044"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/slurmapi"
)

type fakeNodes map[string]slurmapi.Node

func (f fakeNodes) GetNode(name string) (slurmapi.Node, bool) {
	node, found := f[name]
	return node, found
}

func node(states ...api.V0044NodeState) slurmapi.Node {
	stateSet := make(map[api.V0044NodeState]struct{}, len(states))
	for _, state := range states {
		stateSet[state] = struct{}{}
	}
	return slurmapi.Node{States: stateSet}
}

func withTopology(n slurmapi.Node, topology string) slurmapi.Node {
	n.Topology = topology
	return n
}

func TestDiffRegistrations(t *testing.T) {
	desired := map[string]string{
		"worker-0": "tree-ib:fab:leaf-a",
		"worker-1": "tree-ib:fab:leaf-a",
		"worker-2": "tree-ib:fab:leaf-b",
	}

	t.Run("nodes needing the same value are grouped", func(t *testing.T) {
		diff := diffRegistrations(desired, fakeNodes{
			"worker-0": node(api.V0044NodeStateIDLE),
			"worker-1": node(api.V0044NodeStateIDLE),
			"worker-2": node(api.V0044NodeStateIDLE),
		})

		assert.Equal(t, map[string][]string{
			"tree-ib:fab:leaf-a": {"worker-0", "worker-1"},
			"tree-ib:fab:leaf-b": {"worker-2"},
		}, diff)
	})

	t.Run("a node already carrying the value is left alone", func(t *testing.T) {
		diff := diffRegistrations(desired, fakeNodes{
			"worker-0": withTopology(node(api.V0044NodeStateIDLE), "tree-ib:fab:leaf-a"),
			"worker-1": withTopology(node(api.V0044NodeStateIDLE), "tree-ib:fab:leaf-a"),
			"worker-2": withTopology(node(api.V0044NodeStateIDLE), "tree-ib:fab:leaf-b"),
		})

		assert.Empty(t, diff)
	})

	// The whole point of the exercise: a node that lost its registration gets it back.
	t.Run("a node that lost its registration is corrected", func(t *testing.T) {
		diff := diffRegistrations(desired, fakeNodes{
			"worker-0": node(api.V0044NodeStateIDLE),
			"worker-1": withTopology(node(api.V0044NodeStateIDLE), "tree-ib:fab:leaf-a"),
			"worker-2": withTopology(node(api.V0044NodeStateIDLE), "tree-ib:fab:leaf-b"),
		})

		assert.Equal(t, map[string][]string{"tree-ib:fab:leaf-a": {"worker-0"}}, diff)
	})

	// Writing to a powered-down node is exactly what gets discarded by the next reconfigure.
	t.Run("powered down nodes are skipped", func(t *testing.T) {
		diff := diffRegistrations(desired, fakeNodes{
			"worker-0": node(api.V0044NodeStateIDLE, api.V0044NodeStatePOWEREDDOWN),
			"worker-1": node(api.V0044NodeStateIDLE, api.V0044NodeStatePOWERINGDOWN),
			"worker-2": node(api.V0044NodeStateIDLE),
		})

		assert.Equal(t, map[string][]string{"tree-ib:fab:leaf-b": {"worker-2"}}, diff)
	})

	// Slurm preserves the registration of a node that is powering up, so it is safe to write.
	t.Run("powering up nodes are written", func(t *testing.T) {
		diff := diffRegistrations(map[string]string{"worker-0": "tree-ib:fab:leaf-a"}, fakeNodes{
			"worker-0": node(api.V0044NodeStateIDLE, api.V0044NodeStatePOWERINGUP),
		})

		assert.Equal(t, map[string][]string{"tree-ib:fab:leaf-a": {"worker-0"}}, diff)
	})

	t.Run("nodes Slurm does not know are skipped", func(t *testing.T) {
		diff := diffRegistrations(desired, fakeNodes{"worker-2": node(api.V0044NodeStateIDLE)})

		assert.Equal(t, map[string][]string{"tree-ib:fab:leaf-b": {"worker-2"}}, diff)
	})
}

func TestTopologyLoaded(t *testing.T) {
	const structure = "tree-ib=tree:[]:false"

	jailedConfig := func(annotation string, actions ...v1alpha1.UpdateAction) *v1alpha1.JailedConfig {
		return &v1alpha1.JailedConfig{
			ObjectMeta: metav1.ObjectMeta{
				Annotations: map[string]string{consts.AnnotationTopologyStructure: annotation},
			},
			Spec: v1alpha1.JailedConfigSpec{UpdateActions: actions},
		}
	}

	t.Run("open once the committed structure is the rendered one", func(t *testing.T) {
		assert.True(t, topologyLoaded(jailedConfig(structure), structure))
	})

	// While a reconfigure is outstanding the annotation still holds the previous structure, so
	// slurmctld has not read the new topology and a push would be rejected for every node in it.
	t.Run("closed while a reconfigure is outstanding", func(t *testing.T) {
		assert.False(t, topologyLoaded(
			jailedConfig("tree-ib=tree:[]:true", v1alpha1.UpdateActionReconfigure), structure))
	})

	t.Run("closed without a JailedConfig", func(t *testing.T) {
		assert.False(t, topologyLoaded(nil, structure))
	})
}
