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

	base, err = NormalizeSoperatorVersion("v5.0.0+build.1")
	require.NoError(t, err)
	assert.Equal(t, "5.0.0", base)
}

func TestSoperatorVersionBeforeFive(t *testing.T) {
	assert.True(t, SoperatorVersionBeforeFive("4.0.2"))
	assert.True(t, SoperatorVersionBeforeFive("4.1.5"))
	assert.False(t, SoperatorVersionBeforeFive("5.0.0"))
	assert.False(t, SoperatorVersionBeforeFive("5.1.0"))
	assert.False(t, SoperatorVersionBeforeFive(""))
	assert.True(t, SoperatorVersionBeforeFive("4.1.5-reb85d0e5"))
}
