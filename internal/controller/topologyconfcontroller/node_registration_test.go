package topologyconfcontroller

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The registration strings asserted here are the ones a worker computes for itself in its init
// container. Both sides write into the same field, so a disagreement makes them overwrite each
// other on every reconcile. The same expectations are pinned from the worker's side in
// TestRegistrationMatchesTheOperator in images/worker/worker_init_test.py, and the two sets must be
// changed together.

// goldenTreeConfig is one fabric with two leaves under a spine, plus a catch-all for the nodes
// whose labels have not arrived.
const goldenTreeConfig = `- topology: flat
  cluster_default: true
  flat: true
- topology: tree-ib
  cluster_default: false
  tree:
    switches:
      - switch: fab
        children: spine,fab.unknown
      - switch: spine
        children: leaf-a,leaf-b
      - switch: leaf-a
        nodes: worker-[0-1]
      - switch: leaf-b
        nodes: worker-2
      - switch: fab.unknown
        nodes: worker-3
`

func TestDesiredRegistrations(t *testing.T) {
	registrations, err := desiredRegistrations(goldenTreeConfig)
	require.NoError(t, err)

	assert.Equal(t, map[string]string{
		"worker-0": "tree-ib:fab:spine:leaf-a",
		"worker-1": "tree-ib:fab:spine:leaf-a",
		"worker-2": "tree-ib:fab:spine:leaf-b",
	}, registrations)
}

// A node in the catch-all is a node whose placement is not known yet. Registering it would assert
// an answer the operator does not have, and the file already puts it there anyway.
func TestDesiredRegistrationsSkipsTheCatchAll(t *testing.T) {
	registrations, err := desiredRegistrations(goldenTreeConfig)
	require.NoError(t, err)

	assert.NotContains(t, registrations, "worker-3")
}

func TestDesiredRegistrationsOfABlockTopology(t *testing.T) {
	config := `- topology: block-nvl72
  cluster_default: false
  block:
    block_sizes: [2]
    blocks:
      - block: block-0
        nodes: worker-[0-1]
      - block: unknown
        nodes: worker-2
`
	registrations, err := desiredRegistrations(config)
	require.NoError(t, err)

	assert.Equal(t, map[string]string{
		"worker-0": "block-nvl72:block-0",
		"worker-1": "block-nvl72:block-0",
	}, registrations)
}

// A node described by two topologies registers into both, in the order the file declares them.
func TestDesiredRegistrationsJoinsSeveralTopologies(t *testing.T) {
	config := goldenTreeConfig + `- topology: block-nvl72
  cluster_default: false
  block:
    block_sizes: [2]
    blocks:
      - block: block-0
        nodes: worker-[0-1]
`
	registrations, err := desiredRegistrations(config)
	require.NoError(t, err)

	assert.Equal(t, "tree-ib:fab:spine:leaf-a,block-nvl72:block-0", registrations["worker-0"])
	assert.Equal(t, "tree-ib:fab:spine:leaf-b", registrations["worker-2"])
}

// Flat lists no node, so it contributes no registration -- matching what the worker does.
func TestDesiredRegistrationsIgnoresFlat(t *testing.T) {
	registrations, err := desiredRegistrations("- topology: flat\n  cluster_default: true\n  flat: true\n")
	require.NoError(t, err)

	assert.Empty(t, registrations)
}

func TestDesiredRegistrationsOfAMalformedConfig(t *testing.T) {
	_, err := desiredRegistrations("not: [a, yaml, list")

	assert.Error(t, err)
}

// A leaf that is its own ancestor would otherwise walk forever.
func TestDesiredRegistrationsRejectsACycle(t *testing.T) {
	config := `- topology: tree-ib
  cluster_default: false
  tree:
    switches:
      - switch: a
        children: b
      - switch: b
        children: a
      - switch: a
        nodes: worker-0
`
	_, err := desiredRegistrations(config)

	assert.ErrorContains(t, err, "cycle")
}
