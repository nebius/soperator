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

func TestSharedSequentialScenariosHaveSpecificReasons(t *testing.T) {
	var sequential []string
	for _, featurePath := range FeaturePaths() {
		content, err := fs.ReadFile(acceptanceFeatures, featurePath)
		require.NoError(t, err)
		doc, err := gherkin.ParseGherkinDocument(strings.NewReader(string(content)), (&messages.Incrementing{}).NewId)
		require.NoError(t, err)
		for _, tag := range doc.Feature.Tags {
			assert.NotContains(t, []string{"@parallel", "@sequential"}, tag.Name, "concurrency decisions must be scenario-level: %s", featurePath)
		}
		lines := strings.Split(string(content), "\n")
		for _, child := range doc.Feature.Children {
			if child.Scenario == nil {
				continue
			}
			for _, tag := range child.Scenario.Tags {
				assert.NotEqual(t, "@parallel", tag.Name, "scenarios are concurrent by default: %s", featurePath)
				if tag.Name != "@sequential" {
					continue
				}
				sequential = append(sequential, child.Scenario.Name)
				require.Greater(t, int(tag.Location.Line), 1)
				reason := strings.TrimSpace(lines[tag.Location.Line-2])
				assert.True(t, strings.HasPrefix(reason, "# Sequential: ") && len(strings.TrimPrefix(reason, "# Sequential: ")) > 20,
					"explain the concrete conflict immediately above @sequential: %s:%d", featurePath, tag.Location.Line)
			}
		}
	}
	assert.ElementsMatch(t, []string{
		"The provisioned cluster is ready for acceptance tests",
		"Docker containers can run over login SSH",
		"Docker containers can run over SSH on <worker_type> workers",
		"Enroot containers can run over SSH",
		"Login pods scale up without automatic pod scale-down",
		"A static NodeSet can transition to ephemeral mode and back",
		"Installing and uninstalling cowsay preserves GPU tooling",
		"Installing and uninstalling nvitop preserves GPU tooling",
		"A custom Slurm setting is applied and restored",
		"Tailscale login sidecars wait for authentication and persist their state",
		"Partitions and workers use their configured topologies",
		"Every configured topology can schedule a job",
		"A job assigns task ranks in rendered block order",
		"Blocks are sorted according to IB topology labels",
		"A block topology schedules jobs after its base size changes",
		"scontrol topology and SLURM_TOPOLOGY_ADDR agree across workers",
		"A switch limit rejects a cross-leaf allocation",
		"A worker template update preserves running jobs",
	}, sequential)
}

func TestSharedHeavyScenarios(t *testing.T) {
	var heavy []string
	for _, path := range FeaturePaths() {
		content, err := fs.ReadFile(acceptanceFeatures, path)
		require.NoError(t, err)
		assert.NotContains(t, string(content), "@essential")
		doc, err := gherkin.ParseGherkinDocument(strings.NewReader(string(content)), (&messages.Incrementing{}).NewId)
		require.NoError(t, err)
		for _, child := range doc.Feature.Children {
			if child.Scenario == nil {
				continue
			}
			for _, tag := range child.Scenario.Tags {
				if tag.Name == "@heavy" {
					heavy = append(heavy, child.Scenario.Name)
				}
			}
		}
	}
	assert.ElementsMatch(t, []string{
		"GPU ActiveCheck succeeds on all GPU workers",
		"Docker containers can access GPUs",
		"NVIDIA bug report is collected from a worker",
		"Enroot containers can access GPUs",
		"Login pods scale up without automatic pod scale-down",
		"Manage jail dry-run detects drift without modifying the jail",
		"A worker template update preserves running jobs",
		"A maintenance event replaces the selected worker node",
		"A maintenance event replaces a CPU worker node when CPU and GPU workers exist",
		"An MK8s node group update preserves running jobs",
		"A worker update preserves maintenance drains before and during handoff",
		"Non-responding kubelet recreates a Kubernetes node and recovers the worker",
	}, heavy)
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
// bounded to older clusters selects nothing at the current version, which says nothing about
// whether it is runnable, so it is checked at a version its tag actually covers.
var featureTargetVersions = map[string]string{
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
