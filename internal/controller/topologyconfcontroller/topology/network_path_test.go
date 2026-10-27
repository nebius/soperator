package topology

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_NewNetworkPath_SortsByTier(t *testing.T) {
	p, err := NewNetworkPath(
		NetworkPathSegment{Tier: 2, Name: "t2-a0c2e8"},
		NetworkPathSegment{Tier: 0, Name: "t0-3f9c0a"},
		NetworkPathSegment{Tier: 1, Name: "t1-b71e4d"},
	)
	require.NoError(t, err)
	assert.Equal(t, segments("t0-3f9c0a", "t1-b71e4d", "t2-a0c2e8"), p.Segments())
}

func Test_NewNetworkPath_StartsAboveTierZero(t *testing.T) {
	p, err := NewNetworkPath(NetworkPathSegment{Tier: 2, Name: "spine"}, NetworkPathSegment{Tier: 1, Name: "leaf"})
	require.NoError(t, err)
	assert.Equal(t, []NetworkPathSegment{{Tier: 1, Name: "leaf"}, {Tier: 2, Name: "spine"}}, p.Segments())
	assert.Equal(t, "tier-1=leaf/tier-2=spine", p.String())
}

func Test_NewNetworkPath_SkippedTier(t *testing.T) {
	_, err := NewNetworkPath(NetworkPathSegment{Tier: 2, Name: "spine"}, NetworkPathSegment{Tier: 0, Name: "rack"})
	assert.EqualError(t, err, `validate tier-2 segment "spine": tier-1 below it is missing`)
	_, err = NewNetworkPath(NetworkPathSegment{Tier: 0, Name: "rack"}, NetworkPathSegment{Tier: 1, Name: "leaf"}, NetworkPathSegment{Tier: 3, Name: "core"})
	assert.EqualError(t, err, `validate tier-3 segment "core": tier-2 below it is missing`)
}

func Test_NewNetworkPath_NoSegments(t *testing.T) {
	p, err := NewNetworkPath()
	require.NoError(t, err)
	assert.Equal(t, NetworkPath{}, p)
	assert.Empty(t, p.Segments())
	assert.Empty(t, p.String())
}

func Test_NewNetworkPath_TwoSegmentsAtOneTier(t *testing.T) {
	_, err := NewNetworkPath(
		NetworkPathSegment{Tier: 1, Name: "leaf-a"},
		NetworkPathSegment{Tier: 0, Name: "rack"},
		NetworkPathSegment{Tier: 1, Name: "leaf-b"},
	)
	assert.EqualError(t, err, "validate tier-1: two segments at one tier")
}

func Test_NewNetworkPath_UnwritableName(t *testing.T) {
	_, err := NewNetworkPath(NetworkPathSegment{Tier: 0, Name: "rack"}, NetworkPathSegment{Tier: 2, Name: "spine[1]"})
	assert.EqualError(t, err, `validate tier-2 segment "spine[1]": name contains '[', which Slurm's hostlist syntax reserves`)
}

func Test_NewNetworkPath_CopiesSlices(t *testing.T) {
	input := []NetworkPathSegment{{Tier: 1, Name: "leaf"}, {Tier: 0, Name: "rack"}}
	p, err := NewNetworkPath(input...)
	require.NoError(t, err)

	input[0].Name = "changed"
	assert.Equal(t, segments("rack", "leaf"), p.Segments())

	returned := p.Segments()
	returned[0].Name = "changed"
	assert.Equal(t, segments("rack", "leaf"), p.Segments())
}

func Test_NetworkPath_String_Basic(t *testing.T) {
	assert.Equal(t, "tier-0=t0-3f9c0a/tier-1=t1-b71e4d/tier-2=t2-a0c2e8", path("t0-3f9c0a", "t1-b71e4d", "t2-a0c2e8").String())
	assert.Equal(t, "tier-1=t1-b71e4d", path("", "t1-b71e4d").String())
}

func Test_formatSegments_Basic(t *testing.T) {
	assert.Empty(t, formatSegments(nil))
	assert.Equal(t, "tier-2=spine/tier-0=rack", formatSegments([]NetworkPathSegment{{Tier: 2, Name: "spine"}, {Tier: 0, Name: "rack"}}))
}

func Test_validateSegmentName_ValidAndInvalid(t *testing.T) {
	for _, name := range []string{
		"t2-a0c2e8",
		"gpucluster-5e8a1f",
		"66b2be03e8b30ab5bcf8c9fd57d6c293",
		"sw123456789012345678",
		"gpucluster-1f0c.unknown",
		"a-",
		"ü",
	} {
		assert.NoError(t, validateSegmentName(name), name)
	}

	for name, want := range map[string]string{
		"":     "name is empty",
		"a b":  `name contains ' ', which Slurm's hostlist syntax reserves`,
		"a\tb": `name contains '\t', which Slurm's hostlist syntax reserves`,
		"a\nb": `name contains '\n', which Slurm's hostlist syntax reserves`,
		"a,b":  `name contains ',', which Slurm's hostlist syntax reserves`,
		"a[b":  `name contains '[', which Slurm's hostlist syntax reserves`,
		"a]b":  `name contains ']', which Slurm's hostlist syntax reserves`,
		"a{b":  `name contains '{', which Slurm's hostlist syntax reserves`,
		"a}b":  `name contains '}', which Slurm's hostlist syntax reserves`,
	} {
		assert.EqualError(t, validateSegmentName(name), want, name)
	}
}
