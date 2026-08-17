package acceptance

import (
	"io/fs"
	"strings"
	"testing"

	gherkin "github.com/cucumber/gherkin/go/v26"
	messages "github.com/cucumber/messages/go/v21"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"nebius.ai/soperator-e2e/acceptance/framework"
)

func TestSharedFeaturesParseWithGherkin(t *testing.T) {
	for _, path := range FeaturePaths() {
		t.Run(path, func(t *testing.T) {
			content, err := fs.ReadFile(acceptanceFeatures, path)
			require.NoError(t, err)

			_, err = gherkin.ParseGherkinDocument(strings.NewReader(string(content)), (&messages.Incrementing{}).NewId)
			require.NoError(t, err)
		})
	}
}

func TestSharedFeaturesHaveValidVersionTags(t *testing.T) {
	suite := SoperatorSuite("5.0.0")
	runner, err := NewRunner(RunnerConfig{
		KubectlContext:         "dev-context",
		TargetSoperatorVersion: "5.0.0",
		Suites:                 []SuiteConfig{suite},
	})
	require.NoError(t, err)

	paths, err := runner.suiteFeaturePaths(&framework.ClusterInfo{TargetSoperatorVersion: "5.0.0"}, runner.suites[0])
	require.NoError(t, err)
	assert.NotEmpty(t, paths)
}
