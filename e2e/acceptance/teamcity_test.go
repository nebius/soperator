package acceptance

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/framework"
	"github.com/nebius/soperator/e2e/acceptance/internal/reports"
)

func TestRunnerTeamCityReportsCleanupFailuresWithAndWithoutReservations(t *testing.T) {
	for _, reservations := range []bool{false, true} {
		t.Run(fmt.Sprint(reservations), func(t *testing.T) {
			var output bytes.Buffer
			reporter := reports.NewTeamCityReporter(&output)
			var mu sync.Mutex
			var cleanupFailures []error
			config := SuiteConfig{
				Name: "cleanup", WorkerReservations: reservations,
				Source: FeatureSource{FS: fstest.MapFS{"test.feature": {Data: []byte(`Feature: Cleanup reporting
  Scenario: passes
    Given the case runs
  Scenario: clean skip
    Given the case runs
  Scenario: recorded cleanup failure
    Given the case runs
  Scenario: returned cleanup failure
    Given the case runs
  Scenario: step and cleanup fail
    Given the case runs
    And the case runs
`)}}, Paths: []string{"test.feature"}},
				StepRegistrars: []StepRegistrar{func(sc *godog.ScenarioContext, _ *framework.ClusterInfo, _ framework.Runtime) {
					var name string
					sc.Before(func(ctx context.Context, scenario *godog.Scenario) (context.Context, error) {
						name = scenario.Name
						return ctx, nil
					})
					sc.Step(`^the case runs$`, func() error {
						switch name {
						case "passes":
							return nil
						case "step and cleanup fail":
							return errors.New("original step failure")
						default:
							return fmt.Errorf("optional prerequisite absent: %w", godog.ErrSkip)
						}
					})
					sc.After(func(ctx context.Context, _ *godog.Scenario, _ error) (context.Context, error) {
						switch name {
						case "recorded cleanup failure", "step and cleanup fail":
							framework.RecordWorkerCleanupError(ctx, errors.New("restore scenario state"))
						case "returned cleanup failure":
							return ctx, errors.New("returned restoration failure")
						}
						return ctx, nil
					})
				}},
			}
			runner := &Runner{}
			ctx := context.WithValue(t.Context(), reservationManagerContextKey{}, framework.NewReservationManager())
			suite := godog.TestSuite{
				Name: config.Name,
				ScenarioInitializer: func(sc *godog.ScenarioContext) {
					runner.initializeSuiteScenario(sc, testClusterInfo(), nil, config, framework.NewArtifactsManager("", nil), reporter, func(_ string, err error) {
						mu.Lock()
						defer mu.Unlock()
						cleanupFailures = append(cleanupFailures, err)
					})
				},
				Options: &godog.Options{Format: "progress", Output: io.Discard, FS: config.Source.FS, Paths: config.Source.Paths, Strict: true, DefaultContext: ctx, Concurrency: 3},
			}
			assert.Equal(t, 1, suite.Run())
			assert.Len(t, cleanupFailures, 3)
			report := output.String()
			assert.Equal(t, 5, strings.Count(report, "##teamcity[testStarted"))
			assert.Equal(t, 5, strings.Count(report, "##teamcity[testFinished"))
			assert.Equal(t, 3, strings.Count(report, "##teamcity[testFailed"))
			assert.Equal(t, 1, strings.Count(report, "##teamcity[testIgnored"))
			assert.Contains(t, report, "testIgnored name='cleanup: test.feature: clean skip'")
			assert.Contains(t, report, "testFailed name='cleanup: test.feature: recorded cleanup failure'")
			assert.Contains(t, report, "testFailed name='cleanup: test.feature: returned cleanup failure'")
			assert.Contains(t, report, "testFailed name='cleanup: test.feature: step and cleanup fail'")
			assert.Contains(t, report, "original step failure")
			assert.Contains(t, report, "restore scenario state")
			assert.Contains(t, report, "returned restoration failure")
		})
	}
}

func TestTeamCityHooksReportParallelScenarioResults(t *testing.T) {
	var output bytes.Buffer
	reporter := reports.NewTeamCityReporter(&output)
	featureFS := fstest.MapFS{
		"features/teamcity.feature": {Data: []byte(`Feature: TeamCity reporting
  Scenario: passes
    Then the step passes

  @skip
  Scenario: skips
    Then the step passes

  Scenario: fails
    Then the step fails
    And the step passes
`)},
	}
	suite := godog.TestSuite{
		Name: "reporting",
		ScenarioInitializer: func(sc *godog.ScenarioContext) {
			registerTeamCityStateHook(sc, "reporting")
			registerTimingHooks(sc)
			registerSkipHook(sc)
			sc.Step(`^the step passes$`, func() error { return nil })
			sc.Step(`^the step fails$`, func() error { return errors.New("controlled failure") })
			registerTeamCityResultHooks(sc, reporter)
		},
		Options: &godog.Options{
			Format:      "progress",
			Output:      io.Discard,
			FS:          featureFS,
			Paths:       []string{"features/teamcity.feature"},
			Strict:      true,
			Concurrency: 3,
		},
	}

	assert.Equal(t, 1, suite.Run())
	report := output.String()
	assert.Equal(t, 3, strings.Count(report, "##teamcity[testStarted"))
	assert.Equal(t, 3, strings.Count(report, "##teamcity[testFinished"))
	assert.Equal(t, 1, strings.Count(report, "##teamcity[testFailed"))
	assert.Equal(t, 1, strings.Count(report, "##teamcity[testIgnored"))
	assert.Contains(t, report, "name='reporting: features/teamcity.feature: passes'")
	assert.Contains(t, report, "name='reporting: features/teamcity.feature: skips'")
	assert.Contains(t, report, "name='reporting: features/teamcity.feature: fails'")
	assert.Contains(t, report, "testStarted name='reporting: features/teamcity.feature: skips'")
	assert.Contains(t, report, "message='controlled failure'")
	assert.Contains(t, report, "message='scenario skipped at step: the step passes'")

	durations := regexp.MustCompile(`##teamcity\[testFinished [^\n]+ duration='[0-9]+'`).FindAllString(report, -1)
	assert.Len(t, durations, 3)
	assert.NotContains(t, report, "testIgnored name='reporting: features/teamcity.feature: fails'")
	assert.Contains(t, report, "testFinished name='reporting: features/teamcity.feature: skips'")
}

func TestRecordTeamCityStepResultDoesNotReplaceFailureWithSkip(t *testing.T) {
	state := &teamCityScenarioState{}
	step := &godog.Step{Text: "a step"}

	recordTeamCityStepResult(state, step, godog.StepFailed, errors.New("failed first"))
	recordTeamCityStepResult(state, step, godog.StepSkipped, nil)

	require.Equal(t, teamCityScenarioFailed, state.result)
	assert.Equal(t, "failed first", state.message)
}

func TestRecordTeamCityStepResultTreatsIncompleteStatusesAsFailures(t *testing.T) {
	for _, status := range []godog.StepResultStatus{
		godog.StepUndefined,
		godog.StepPending,
		godog.StepAmbiguous,
	} {
		t.Run(status.String(), func(t *testing.T) {
			state := &teamCityScenarioState{}
			recordTeamCityStepResult(state, &godog.Step{Text: "incomplete"}, status, nil)
			assert.Equal(t, teamCityScenarioFailed, state.result)
			assert.Equal(t, `step "incomplete" finished with status `+status.String(), state.message)
		})
	}
}
