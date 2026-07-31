package framework

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNormalizeSoperatorVersion(t *testing.T) {
	base, err := NormalizeSoperatorVersion("4.1.5-reb85d0e5")
	require.NoError(t, err)
	assert.Equal(t, "4.1.5", base)

	base, err = NormalizeSoperatorVersion("v4.2.0+build.1")
	require.NoError(t, err)
	assert.Equal(t, "4.2.0", base)
}

func TestParseSoperatorBaseVersionRejectsShortVersion(t *testing.T) {
	_, err := ParseSoperatorBaseVersion("4.2")
	require.Error(t, err)
	assert.ErrorContains(t, err, "full major.minor.patch")
}
