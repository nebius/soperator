package acceptance

import (
	"bytes"
	"io/fs"
	"regexp"
	"slices"
	"strings"
	"testing"

	gherkin "github.com/cucumber/gherkin/go/v42"
	"github.com/cucumber/godog"
	messages "github.com/cucumber/messages/go/v34"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
	"github.com/nebius/soperator/e2e/acceptance/internal/versionfilter"
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

func TestSequentialTagsHaveScenarioLevelReasons(t *testing.T) {
	paths, err := fs.Glob(acceptanceFeatures, "features/*.feature")
	require.NoError(t, err)
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			content, err := fs.ReadFile(acceptanceFeatures, path)
			require.NoError(t, err)
			doc, err := gherkin.ParseGherkinDocument(strings.NewReader(string(content)), (&messages.Incrementing{}).NewId)
			require.NoError(t, err)
			lines := strings.Split(string(content), "\n")
			checkTags := func(tags []*messages.Tag, scenarioLevel bool) {
				for _, tag := range tags {
					assert.NotEqual(t, "@parallel", tag.Name, "use untagged scenarios for the concurrency-capable suite")
					if tag.Name != "@sequential" {
						continue
					}
					assert.True(t, scenarioLevel, "@sequential must be on a scenario")
					require.Greater(t, tag.Location.Line, int64(1))
					reason := strings.TrimSpace(lines[tag.Location.Line-2])
					assert.True(t, strings.HasPrefix(reason, "# Sequential: ") && strings.TrimSpace(strings.TrimPrefix(reason, "# Sequential: ")) != "", "line %d: add a concrete # Sequential: reason immediately above the tag", tag.Location.Line)
				}
			}
			checkTags(doc.Feature.Tags, false)
			for _, child := range doc.Feature.Children {
				if child.Scenario != nil {
					checkTags(child.Scenario.Tags, true)
					for _, examples := range child.Scenario.Examples {
						checkTags(examples.Tags, false)
					}
				}
				if child.Rule != nil {
					checkTags(child.Rule.Tags, false)
					for _, ruleChild := range child.Rule.Children {
						if ruleChild.Scenario != nil {
							checkTags(ruleChild.Scenario.Tags, true)
							for _, examples := range ruleChild.Scenario.Examples {
								checkTags(examples.Tags, false)
							}
						}
					}
				}
			}
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

// manualFeatures are embedded scenarios deliberately kept out of the default suite: they need a
// cluster shaped a particular way, or they change the cluster while they run, so they are started
// by hand with --scenario instead of by every e2e run.
var manualFeatures []string

// TestEveryEmbeddedFeatureIsRegisteredOrManual keeps a new feature file from being embedded and
// then silently never running: it either belongs to the default suite or is declared manual here.
func TestEveryEmbeddedFeatureIsRegisteredOrManual(t *testing.T) {
	paths, err := fs.Glob(acceptanceFeatures, "features/*.feature")
	require.NoError(t, err)
	require.NotEmpty(t, paths)

	registered := FeaturePaths()
	for _, path := range paths {
		if slices.Contains(manualFeatures, path) {
			assert.NotContains(t, registered, path, "manual feature must stay out of the default suite")
			continue
		}
		assert.Contains(t, registered, path)
	}
}

// featureTargetVersions overrides the Soperator version a feature is checked against. A feature
// whose tag excludes the default version selects nothing there, which says nothing about
// whether it is runnable, so it is checked at a version its tag actually covers.
var featureTargetVersions = map[string]string{
	"features/login_updates.feature":   "5.1.0",
	"features/topology_legacy.feature": "4.0.0",
}

const defaultFeatureTargetVersion = "5.0.0"

// TestEmbeddedFeaturesAreRunnable covers the manual features too: they reach Godog through
// --scenario, so they have to parse and carry the version tags the runner requires.
func TestEmbeddedFeaturesAreRunnable(t *testing.T) {
	paths, err := fs.Glob(acceptanceFeatures, "features/*.feature")
	require.NoError(t, err)

	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			content, err := fs.ReadFile(acceptanceFeatures, path)
			require.NoError(t, err)

			_, err = gherkin.ParseGherkinDocument(strings.NewReader(string(content)), (&messages.Incrementing{}).NewId)
			require.NoError(t, err)

			target := defaultFeatureTargetVersion
			if override, ok := featureTargetVersions[path]; ok {
				target = override
			}
			selected, err := versionfilter.SelectScenarios(
				versionfilter.FeatureSource{FS: acceptanceFeatures, Paths: []string{path}},
				versionfilter.Axis{TagPrefix: SoperatorVersionTagPrefix, TargetVersion: target},
			)
			require.NoError(t, err)
			assert.NotEmpty(t, selected)
		})
	}
}

func TestLoginUpdatesVersionAndSequentialFiltering(t *testing.T) {
	for _, test := range []struct {
		version string
		cases   int
	}{
		{version: "4.1.14", cases: 0},
		{version: "5.0.0", cases: 0},
		{version: "5.0.0-rabcdef", cases: 0},
		{version: "5.0.1", cases: 0},
		{version: "5.1.0", cases: 3},
	} {
		t.Run(test.version, func(t *testing.T) {
			suite := SoperatorSuite(test.version)
			suite.Source.Paths = []string{"features/login_updates.feature"}
			selected := selectedCases(t, suite, test.version)
			require.Len(t, selected, test.cases)

			partitions := ConcurrentSuites(suite, 1)
			require.Len(t, partitions, 2)
			assert.Equal(t, selected, selectedCases(t, partitions[0], test.version))
			assert.Empty(t, selectedCases(t, partitions[1], test.version))
		})
	}
}

// TestEveryEmbeddedStepHasADefinition matches every step of every embedded feature against the
// shared step registrar. An undefined step otherwise surfaces only once the suite runs against a
// real cluster, which is the most expensive place to discover it.
func TestEveryEmbeddedStepHasADefinition(t *testing.T) {
	definitions := registeredStepDefinitions(t)
	require.NotEmpty(t, definitions)

	paths, err := fs.Glob(acceptanceFeatures, "features/*.feature")
	require.NoError(t, err)

	for _, path := range paths {
		content, err := fs.ReadFile(acceptanceFeatures, path)
		require.NoError(t, err)
		doc, err := gherkin.ParseGherkinDocument(strings.NewReader(string(content)), (&messages.Incrementing{}).NewId)
		require.NoError(t, err)

		pickles := gherkin.Pickles(*doc, path, (&messages.Incrementing{}).NewId)
		for _, pickle := range pickles {
			for _, step := range pickle.Steps {
				assertStepIsDefined(t, definitions, path, step.Text)
			}
		}
	}
}

func assertStepIsDefined(t *testing.T, definitions []*regexp.Regexp, path, step string) {
	t.Helper()

	assert.True(t, slices.ContainsFunc(definitions, func(expr *regexp.Regexp) bool {
		return expr.MatchString(step)
	}), "%s: step %q has no definition", path, step)
}

// registeredStepDefinitions asks Godog to print the expressions the shared registrar installs.
// Registration alone touches no cluster, so the families are built with a nil runtime.
func registeredStepDefinitions(t *testing.T) []*regexp.Regexp {
	t.Helper()

	var printed bytes.Buffer
	suite := godog.TestSuite{
		Name: "definitions",
		ScenarioInitializer: func(sc *godog.ScenarioContext) {
			RegisterSharedSteps(sc, &framework.ClusterInfo{
				SlurmClusterName:       defaultSlurmClusterName,
				TargetSoperatorVersion: "5.0.0",
			}, nil)
		},
		Options: &godog.Options{
			Format:              "progress",
			ShowStepDefinitions: true,
			NoColors:            true,
			Output:              &printed,
		},
	}
	suite.Run()

	// printStepDefinitions colours its output regardless of NoColors.
	ansi := regexp.MustCompile("\x1b\\[[0-9;]*m")

	var expressions []*regexp.Regexp
	for _, line := range strings.Split(printed.String(), "\n") {
		expr, _, found := strings.Cut(ansi.ReplaceAllString(line, ""), "# ")
		if !found {
			continue
		}
		expr = strings.TrimSpace(expr)
		if expr == "" {
			continue
		}
		compiled, err := regexp.Compile(expr)
		require.NoError(t, err, "step expression %q", expr)
		expressions = append(expressions, compiled)
	}
	return expressions
}
