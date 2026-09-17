package acceptance

import (
	"bytes"
	"errors"
	"io"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/cucumber/godog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/nebius/soperator/e2e/acceptance/internal/reports"
)

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
			registerTeamCityStateHook(sc, reporter, "reporting")
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
	assert.Equal(t, 2, strings.Count(report, "##teamcity[testStarted"))
	assert.Equal(t, 2, strings.Count(report, "##teamcity[testFinished"))
	assert.Equal(t, 1, strings.Count(report, "##teamcity[testFailed"))
	assert.Equal(t, 1, strings.Count(report, "##teamcity[testIgnored"))
	assert.Contains(t, report, "name='reporting: features/teamcity.feature: passes'")
	assert.Contains(t, report, "name='reporting: features/teamcity.feature: skips'")
	assert.Contains(t, report, "name='reporting: features/teamcity.feature: fails'")
	assert.Contains(t, report, "message='controlled failure'")
	assert.Contains(t, report, "message='scenario skipped at step: the step passes'")

	durations := regexp.MustCompile(`##teamcity\[testFinished [^\n]+ duration='[0-9]+'`).FindAllString(report, -1)
	assert.Len(t, durations, 2)
	assert.NotContains(t, report, "testIgnored name='reporting: features/teamcity.feature: fails'")
	assert.NotContains(t, report, "testFinished name='reporting: features/teamcity.feature: skips'")
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
