//go:build helmapi

package helmapi

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Masterminds/semver/v3"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

type collectorChartMetadata struct{ Name, Version string }

func readCollectorChartMetadata(t *testing.T, chart string) collectorChartMetadata {
	t.Helper()
	var metadata collectorChartMetadata
	require.NoError(t, yaml.Unmarshal(commandOutput(t, "helm", "show", "chart", chart), &metadata))
	return metadata
}

func verifyCollectorChart(t *testing.T, chart, release string, source collectorChartSource) {
	t.Helper()
	metadata := readCollectorChartMetadata(t, chart)
	require.Equal(t, source.name, metadata.Name)
	constraint, err := semver.NewConstraint(source.version)
	require.NoError(t, err)
	version, err := semver.NewVersion(metadata.Version)
	require.NoError(t, err)
	require.Truef(t, constraint.Check(version), "%s requires %s %s, got %s", release, source.name, source.version, version)
	t.Logf("%s: %s %s from %s, testing %s", release, source.name, source.version, source.repository, version)
}

func pullCollectorChart(t *testing.T, cacheDir, repositoryURL, chartName, constraint string) string {
	t.Helper()
	dir, err := os.MkdirTemp(cacheDir, "collector-")
	require.NoError(t, err)
	repoFlags := []string{
		"--repository-config", filepath.Join(dir, "repositories.yaml"),
		"--repository-cache", dir,
		"--content-cache", filepath.Join(dir, "content"),
	}
	commandOutput(t, "helm", append(repoFlags, "repo", "add", "relay", repositoryURL)...)
	commandOutput(t, "helm", append(repoFlags, "pull", "relay/"+chartName, "--version", constraint, "--destination", dir)...)
	archives, err := filepath.Glob(filepath.Join(dir, "*.tgz"))
	require.NoError(t, err)
	require.Len(t, archives, 1, "pull %s %s from %s", chartName, constraint, repositoryURL)
	archive := archives[0]

	metadata := readCollectorChartMetadata(t, archive)
	var index struct {
		Entries map[string][]struct{ Version, Digest string }
	}
	indexData, err := os.ReadFile(filepath.Join(dir, "relay-index.yaml"))
	require.NoError(t, err)
	require.NoError(t, yaml.Unmarshal(indexData, &index))
	for _, entry := range index.Entries[chartName] {
		if entry.Version == metadata.Version {
			require.NotEmpty(t, entry.Digest, "missing digest for %s %s in %s", chartName, metadata.Version, repositoryURL)
			data, err := os.ReadFile(archive)
			require.NoError(t, err)
			require.Equal(t, strings.TrimPrefix(entry.Digest, "sha256:"), fmt.Sprintf("%x", sha256.Sum256(data)),
				"repository digest for %s %s", chartName, metadata.Version)
			return archive
		}
	}
	t.Fatalf("repository %s has no index entry for %s %s", repositoryURL, chartName, metadata.Version)
	return ""
}
