package topology

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_NewSpec_EachKind(t *testing.T) {
	for kind, want := range map[string]topologyType{
		"flat":  topologyTypeFlat,
		"tree":  topologyTypeTree,
		"block": topologyTypeBlock,
	} {
		spec, err := NewSpec("t", kind, true, nil, []string{"worker"})
		require.NoError(t, err, kind)
		assert.Equal(t, Spec{name: "t", kind: want, clusterDefault: true, nodeSetNames: []string{"worker"}}, spec, kind)
	}
}

func Test_NewSpec_BlockSizes(t *testing.T) {
	spec, err := NewSpec("gb300", "block", false, []int{18, 36}, nil)
	require.NoError(t, err)
	assert.Equal(t, []int{18, 36}, spec.blockSizes)
	assert.Nil(t, spec.nodeSetNames)
	assert.False(t, spec.clusterDefault)
}

func Test_NewSpec_EmptyName(t *testing.T) {
	_, err := NewSpec("", "tree", true, nil, nil)
	assert.EqualError(t, err, "validate topology spec: name is empty")
}

func Test_NewSpec_UnsupportedKind(t *testing.T) {
	for _, kind := range []string{"torus3d", "", "Tree"} {
		_, err := NewSpec("t", kind, true, nil, nil)
		assert.EqualError(t, err, `validate topology spec "t": unsupported type "`+kind+`"`, kind)
	}
}

func Test_NewSpec_BlockSizesForFlatAndTree(t *testing.T) {
	for _, kind := range []string{"flat", "tree"} {
		_, err := NewSpec("t", kind, true, []int{18}, nil)
		assert.EqualError(t, err, `validate topology spec "t": block sizes are only for block topologies`, kind)
	}
}

func Test_NewSpec_BlockSizeError(t *testing.T) {
	_, err := NewSpec("gb300", "block", true, []int{18, 54}, nil)
	assert.EqualError(t, err, `validate topology spec "gb300": validate block size 1: 54 is not a power-of-two multiple of the previous size 18`)
}

func Test_NewSpec_CopiesSlices(t *testing.T) {
	sizes := []int{18, 36}
	names := []string{"worker-rack0"}
	spec, err := NewSpec("gb300", "block", true, sizes, names)
	require.NoError(t, err)

	sizes[0] = 1
	names[0] = "changed"
	assert.Equal(t, []int{18, 36}, spec.blockSizes)
	assert.Equal(t, []string{"worker-rack0"}, spec.nodeSetNames)
}

func Test_Spec_Name_Basic(t *testing.T) {
	assert.Equal(t, "tree-gpu", mustSpec("tree-gpu", "tree", true, nil, nil).Name())
}

func Test_validateBlockSizes_ValidAndInvalid(t *testing.T) {
	for _, sizes := range [][]int{nil, {}, {18}, {18, 36, 72}, {4, 16}, {3, 6, 12}, {1, 2, 4, 8}} {
		assert.NoError(t, validateBlockSizes(sizes), "%v", sizes)
	}

	for want, sizes := range map[string][]int{
		"validate block size 0: must be positive":                                           {0},
		"validate block size 2: must be positive":                                           {4, 8, -16},
		"validate block size 1: must be positive":                                           {4, 0},
		"validate block size 1: 4 is not a power-of-two multiple of the previous size 4":    {4, 4},
		"validate block size 1: 2 is not a power-of-two multiple of the previous size 4":    {4, 2},
		"validate block size 1: 12 is not a power-of-two multiple of the previous size 4":   {4, 12},
		"validate block size 1: 54 is not a power-of-two multiple of the previous size 18":  {18, 54},
		"validate block size 2: 24 is not a power-of-two multiple of the previous size 8":   {4, 8, 24},
		"validate block size 2: 100 is not a power-of-two multiple of the previous size 36": {18, 36, 100},
	} {
		assert.EqualError(t, validateBlockSizes(sizes), want, "%v", sizes)
	}
}
