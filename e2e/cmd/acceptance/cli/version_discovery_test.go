package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSoperatorVersionFromHelmReleaseRevisionsUsesLastAppliedRevision(t *testing.T) {
	version, err := soperatorVersionFromHelmReleaseRevisions(" 5.0.0-reb85d0e5\n5.0.0-reb000000")
	require.NoError(t, err)
	assert.Equal(t, "5.0.0-reb85d0e5", version)
}

func TestSoperatorVersionFromHelmReleaseRevisionsFallsBackToLastAttemptedRevision(t *testing.T) {
	version, err := soperatorVersionFromHelmReleaseRevisions("\n 5.0.0-reb85d0e5")
	require.NoError(t, err)
	assert.Equal(t, "5.0.0-reb85d0e5", version)
}

func TestSoperatorVersionFromHelmReleaseRevisionsRequiresValue(t *testing.T) {
	_, err := soperatorVersionFromHelmReleaseRevisions(" \n ")
	require.Error(t, err)
	assert.ErrorContains(t, err, "status.lastAppliedRevision and status.lastAttemptedRevision")
}

func TestSoperatorVersionFromHelmReleaseRevisionsRejectsInvalidVersion(t *testing.T) {
	_, err := soperatorVersionFromHelmReleaseRevisions("5.0\n")
	require.Error(t, err)
	assert.ErrorContains(t, err, "unsupported deployed version")
}
