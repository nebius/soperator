package topologyconfcontroller

import (
	"testing"

	"github.com/go-logr/logr"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
)

// requestedHash is the applied hash a request in these tests is raised against. A reconfigure has
// reached slurmctld only once Status.AppliedHash has moved past it.
const requestedHash = "h1"

// jailedConfig builds a JailedConfig whose request, if any, was raised against requestedHash and for
// which nothing new has been applied since.
func jailedConfig(structure string, actions []v1alpha1.UpdateAction) *v1alpha1.JailedConfig {
	jc := &v1alpha1.JailedConfig{}
	jc.Annotations = map[string]string{
		consts.AnnotationTopologyStructure:   structure,
		consts.AnnotationTopologyAppliedHash: requestedHash,
	}
	jc.Spec.UpdateActions = actions
	jc.Status.AppliedHash = requestedHash
	return jc
}

// withReconfigurePerformed marks a reconfigure as done over the content identified by appliedHash.
func withReconfigurePerformed(jc *v1alpha1.JailedConfig, appliedHash string) *v1alpha1.JailedConfig {
	jc.Status.Conditions = append(jc.Status.Conditions, metav1.Condition{
		Type:   string(v1alpha1.ReconfigurePerformed),
		Status: metav1.ConditionTrue,
		Reason: v1alpha1.ReasonSuccess,
	})
	jc.Status.AppliedHash = appliedHash
	return jc
}

var reconfigure = []v1alpha1.UpdateAction{v1alpha1.UpdateActionReconfigure}

func TestReconcileReconfigureRequest(t *testing.T) {
	logger := logr.Discard()

	t.Run("a changed structure asks for a reconfigure", func(t *testing.T) {
		existing := jailedConfig("old", nil)

		assert.Equal(t, reconfigure, reconcileReconfigureRequest(logger, existing, "new"))
	})

	t.Run("an unchanged structure asks for nothing", func(t *testing.T) {
		existing := jailedConfig("same", nil)

		assert.Empty(t, reconcileReconfigureRequest(logger, existing, "same"))
	})

	t.Run("the request stands until a reconfigure is confirmed", func(t *testing.T) {
		existing := jailedConfig("same", reconfigure)

		assert.Equal(t, reconfigure, reconcileReconfigureRequest(logger, existing, "same"),
			"withdrawing before confirmation would race with sconfigcontroller reading the spec")
	})

	t.Run("a confirmation over the content it was raised against does not withdraw it", func(t *testing.T) {
		existing := withReconfigurePerformed(jailedConfig("same", reconfigure), "h1")

		assert.Equal(t, reconfigure, reconcileReconfigureRequest(logger, existing, "same"),
			"that reconfigure ran before this request's content was published")
	})

	t.Run("a confirmation over newer content withdraws it", func(t *testing.T) {
		existing := withReconfigurePerformed(jailedConfig("same", reconfigure), "h2")

		assert.Empty(t, reconcileReconfigureRequest(logger, existing, "same"))
	})

	t.Run("a structure change re-asks even right after a confirmation", func(t *testing.T) {
		existing := withReconfigurePerformed(jailedConfig("old", nil), "h2")

		assert.Equal(t, reconfigure, reconcileReconfigureRequest(logger, existing, "new"))
	})
}

// TestRequestedAtAppliedHash pins the marker that tells "applied since the request" from "applied
// before it". Generation cannot: the request lives in the spec and the structure in an annotation,
// so a second structural change raised against an outstanding request leaves Generation untouched.
func TestRequestedAtAppliedHash(t *testing.T) {
	t.Run("a changed structure re-pins to the current applied hash", func(t *testing.T) {
		existing := withReconfigurePerformed(jailedConfig("old", reconfigure), "h2")

		assert.Equal(t, "h2", requestedAtAppliedHash(existing, "new"),
			"the new request must not inherit the previous request's marker")
	})

	t.Run("an outstanding request holds its marker steady", func(t *testing.T) {
		existing := jailedConfig("same", reconfigure)

		assert.Equal(t, requestedHash, requestedAtAppliedHash(existing, "same"))
	})

	t.Run("with nothing outstanding the marker follows the applied hash", func(t *testing.T) {
		existing := jailedConfig("same", nil)
		existing.Status.AppliedHash = "h9"

		assert.Equal(t, "h9", requestedAtAppliedHash(existing, "same"))
	})
}

// TestReconfigureSurvivesASecondChange walks the sequence that used to lose a reconfigure: a
// structural change arriving while an earlier request is still outstanding.
func TestReconfigureSurvivesASecondChange(t *testing.T) {
	logger := logr.Discard()

	// A reconfigure ran for structure B, whose content hashes to h2.
	existing := withReconfigurePerformed(jailedConfig("B", reconfigure), "h2")

	// The structure now changes to C. The request is already in the spec, so Generation does not
	// move; only the annotations do.
	assert.Equal(t, reconfigure, reconcileReconfigureRequest(logger, existing, "C"))
	existing.Annotations[consts.AnnotationTopologyStructure] = committedStructure(existing, "C")
	existing.Annotations[consts.AnnotationTopologyAppliedHash] = requestedAtAppliedHash(existing, "C")

	assert.Equal(t, reconfigure, reconcileReconfigureRequest(logger, existing, "C"),
		"C has not reached slurmctld yet, so the request must stand")

	// sconfigcontroller applies C.
	existing.Status.AppliedHash = "h3"

	assert.Empty(t, reconcileReconfigureRequest(logger, existing, "C"),
		"only now is the request satisfied")
}

// TestTopologyStructure pins what counts as a structural change: only what slurmctld can learn by
// re-reading the config. A node moving between switches is pushed in with `scontrol update` and
// must not restart slurmd across the cluster.
func TestTopologyStructure(t *testing.T) {
	structureOf := func(entries ...topologyYAMLEntry) string {
		rendered, err := renderTopologyYAML(entries)
		require.NoError(t, err)
		structure, err := topologyStructure(rendered)
		require.NoError(t, err)
		return structure
	}
	base := []topologyYAMLEntry{
		{Topology: "flat", ClusterDefault: true, Flat: true},
		{
			Topology: "block-nvl72",
			Block: &blockTopologyYAML{
				BlockSizes: []int{18},
				Blocks:     []blockYAML{{Block: "block1", Nodes: "h100-0"}},
			},
		},
	}

	t.Run("node membership is not part of it", func(t *testing.T) {
		same := []topologyYAMLEntry{base[0], {
			Topology: "block-nvl72",
			Block: &blockTopologyYAML{
				BlockSizes: []int{18},
				Blocks:     []blockYAML{{Block: "another-block", Nodes: "h100-[1-3]"}},
			},
		}}

		assert.Equal(t, structureOf(base...), structureOf(same...))
	})

	t.Run("adding a topology changes it", func(t *testing.T) {
		added := append(append([]topologyYAMLEntry{}, base...), topologyYAMLEntry{
			Topology: "tree-ib",
			Tree:     &treeTopologyYAML{Switches: []switchYAML{{Switch: "root"}}},
		})

		assert.NotEqual(t, structureOf(base...), structureOf(added...))
	})

	t.Run("changing block sizes changes it", func(t *testing.T) {
		resized := []topologyYAMLEntry{base[0], {
			Topology: "block-nvl72",
			Block: &blockTopologyYAML{
				BlockSizes: []int{36},
				Blocks:     []blockYAML{{Block: "block1", Nodes: "h100-0"}},
			},
		}}

		assert.NotEqual(t, structureOf(base...), structureOf(resized...))
	})

	t.Run("moving the cluster default changes it", func(t *testing.T) {
		moved := append([]topologyYAMLEntry{}, base...)
		moved[0].ClusterDefault = false

		assert.NotEqual(t, structureOf(base...), structureOf(moved...))
	})

	t.Run("an empty file has no structure", func(t *testing.T) {
		assert.Empty(t, structureOf())
	})

	t.Run("replacing placeholder block sizes changes it", func(t *testing.T) {
		placeholder := topologyYAMLEntry{
			Topology: "block-nvl72",
			Block: &blockTopologyYAML{
				BlockSizes: []int{placeholderBlockSizes},
				Blocks:     []blockYAML{{Block: "unknown"}},
			},
		}
		populated := topologyYAMLEntry{
			Topology: "block-nvl72",
			Block: &blockTopologyYAML{
				Blocks: []blockYAML{{Block: "block1", Nodes: "h100-0"}},
			},
		}

		assert.NotEqual(t, structureOf(placeholder), structureOf(populated))
	})
}

// TestCommittedStructure pins how the recorded structure behaves while a reconfigure request is
// outstanding.
func TestCommittedStructure(t *testing.T) {
	t.Run("nothing requested, so the new structure is recorded", func(t *testing.T) {
		existing := jailedConfig("old", nil)

		assert.Equal(t, "new", committedStructure(existing, "new"))
	})

	t.Run("a request still outstanding keeps the old structure", func(t *testing.T) {
		existing := jailedConfig("old", reconfigure)

		assert.Equal(t, "old", committedStructure(existing, "new"),
			"the change is not handled until a reconfigure confirms it")
	})

	t.Run("a confirmed request records the new structure", func(t *testing.T) {
		existing := withReconfigurePerformed(jailedConfig("old", reconfigure), "h2")

		assert.Equal(t, "new", committedStructure(existing, "new"))
	})

	t.Run("a confirmation over older content keeps the old structure", func(t *testing.T) {
		existing := withReconfigurePerformed(jailedConfig("old", reconfigure), "h1")

		assert.Equal(t, "old", committedStructure(existing, "new"))
	})
}
