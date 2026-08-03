package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSoperatorVersionFromHelmReleaseRevision(t *testing.T) {
	version, err := soperatorVersionFromHelmReleaseRevision(" 4.1.5-reb85d0e5\n")
	require.NoError(t, err)
	assert.Equal(t, "4.1.5-reb85d0e5", version)
}

func TestSoperatorVersionFromHelmReleaseRevisionRequiresValue(t *testing.T) {
	_, err := soperatorVersionFromHelmReleaseRevision(" ")
	require.Error(t, err)
	assert.ErrorContains(t, err, "status.lastAttemptedRevision")
}

func TestSoperatorVersionFromHelmReleaseRevisionRejectsInvalidVersion(t *testing.T) {
	_, err := soperatorVersionFromHelmReleaseRevision("4.2")
	require.Error(t, err)
	assert.ErrorContains(t, err, "unsupported deployed version")
}
