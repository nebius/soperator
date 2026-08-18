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

func TestSoperatorPodNameUsesLegacyUnprefixedNamesBeforeFive(t *testing.T) {
	assert.Equal(t, "login-0", SoperatorPodName("soperator", "4.0.2", "login-0"))
	assert.Equal(t, "controller-0", SoperatorPodName("custom", "4.1.5", "controller-0"))
}

func TestSoperatorPodNameUsesClusterPrefixedNamesForFiveAndLater(t *testing.T) {
	assert.Equal(t, "soperator-login-0", SoperatorPodName("soperator", "5.0.0", "login-0"))
	assert.Equal(t, "custom-controller-0", SoperatorPodName("custom", "5.1.0", "controller-0"))
}

func TestSoperatorPodNameKeepsExistingFallbackForUnknownVersion(t *testing.T) {
	assert.Equal(t, "soperator-login-0", SoperatorPodName("soperator", "", "login-0"))
	assert.Equal(t, "login-0", SoperatorPodName("", "5.0.0", "login-0"))
}
