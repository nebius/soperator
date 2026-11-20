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

// sharedScenariosTagged returns the names of default-suite scenarios carrying the tag.
func sharedScenariosTagged(t *testing.T, tag string) []string {
	t.Helper()

	var names []string
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
			for _, scenarioTag := range child.Scenario.Tags {
				if scenarioTag.Name == tag {
					names = append(names, child.Scenario.Name)
				}
			}
		}
	}
	return names
}

func TestSharedHeavyScenarios(t *testing.T) {
	assert.ElementsMatch(t, []string{
		"Accounting privacy preserves access to a Linux user's own records",
		"GPU ActiveCheck succeeds on all GPU workers",
		"nvidia_libraries drains a GPU worker whose <library> is <failure> and recovers after restoration",
		"Docker containers can access GPUs",
		"NVIDIA bug report is collected from a worker",
		"Enroot containers can access GPUs",
		"A worker restarts with user content on image storage and containers keep working for other users",
		"Login pods scale up without automatic pod scale-down",
		"A login user exceeding the memory limit is OOM-killed without affecting other users or login services",
		"Job memory exhaustion leaves neighboring workloads healthy",
		"Manage jail dry-run detects drift without modifying the jail",
		"A worker template update preserves running jobs",
		"A maintenance event replaces the selected worker node",
		"A maintenance event replaces a CPU worker node when CPU and GPU workers exist",
		"A failed GPU health check replaces the worker node",
		"A GPU worker recovers when maintenance arrives during health-check draining",
		"A GPU worker recovers when maintenance arrives after health-check draining",
		"An MK8s node group update preserves running jobs",
		"A worker update preserves maintenance drains before and during handoff",
		"Non-responding kubelet recreates a Kubernetes node and recovers the worker",
		"A manually powered down static worker returns to service",
		"A burst of batch jobs is accepted and drained within the budget",
		"Workers recover after their VMs are stopped and started",
	}, sharedScenariosTagged(t, "@heavy"))
}

func TestSharedSequentialScenarios(t *testing.T) {
	assert.ElementsMatch(t, []string{
		"Periodic HealthCheckProgram passes GPU health checks on all GPU workers",
		"The provisioned cluster is ready for acceptance tests",
		"A custom Slurm setting is applied and restored",
		"Installing and uninstalling cowsay preserves GPU tooling",
		"Installing and uninstalling nvitop preserves GPU tooling",
		"Docker containers can run over login SSH",
		"Docker containers can run over SSH on <worker_type> workers",
		"Enroot containers can run over SSH",
		"Workers expose local NVMe scratch to jobs",
		"Login pods scale up without automatic pod scale-down",
		"A login user exceeding the memory limit is OOM-killed without affecting other users or login services",
		"Job memory exhaustion leaves neighboring workloads healthy",
		"A burst of batch jobs is accepted and drained within the budget",
		"Workers recover after their VMs are stopped and started",
		"A static NodeSet can transition to ephemeral mode and back",
		"A manually powered down static worker returns to service",
		"Tailscale login sidecars wait for authentication and persist their state",
		"Partitions and workers use their configured topologies",
		"Every configured topology can schedule a job",
		"A job assigns task ranks in rendered block order",
		"Blocks are sorted according to IB topology labels",
		"A block topology schedules jobs after its base size changes",
		"A switch limit rejects a cross-leaf allocation",
		"scontrol topology and SLURM_TOPOLOGY_ADDR agree across workers",
		"A worker template update preserves running jobs",
		"An MK8s node group update preserves running jobs",
		"A worker update preserves maintenance drains before and during handoff",
		"A failed GPU health check replaces the worker node",
		"A GPU worker recovers when maintenance arrives during health-check draining",
		"A GPU worker recovers when maintenance arrives after health-check draining",
		"GPU ActiveCheck succeeds on all GPU workers",
	}, sharedScenariosTagged(t, "@sequential"))
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

// Manual scenarios never run unless named with --scenario; keep the list short and
// every entry justified by a comment next to the tag in its feature file.
func TestSharedManualScenarios(t *testing.T) {
	assert.ElementsMatch(t, []string{
		"A failed GPU health check replaces the worker node",
		"A GPU worker recovers when maintenance arrives during health-check draining",
		"A GPU worker recovers when maintenance arrives after health-check draining",
	}, sharedScenariosTagged(t, "@manual"))
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
