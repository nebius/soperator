package acceptance

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/cucumber/godog"

	"github.com/nebius/soperator/e2e/acceptance/internal/reports"
)

type teamCityScenarioResult uint8

const (
	teamCityScenarioPassed teamCityScenarioResult = iota
	teamCityScenarioSkipped
	teamCityScenarioFailed
)

type teamCityScenarioState struct {
	name      string
	flowID    string
	startedAt time.Time
	result    teamCityScenarioResult
	message   string
}

type teamCityScenarioStateKey struct{}

func registerTeamCityStateHook(sc *godog.ScenarioContext, reporter *reports.TeamCityReporter, suiteName string) {
	sc.Before(func(ctx context.Context, scenario *godog.Scenario) (context.Context, error) {
		state := &teamCityScenarioState{
			name:      fmt.Sprintf("%s: %s: %s", suiteName, scenario.Uri, scenario.Name),
			flowID:    suiteName + "/" + scenario.Id,
			startedAt: reporter.Now(),
		}
		return context.WithValue(ctx, teamCityScenarioStateKey{}, state), nil
	})
}

func registerTeamCityResultHooks(sc *godog.ScenarioContext, reporter *reports.TeamCityReporter) {
	sc.StepContext().After(func(ctx context.Context, step *godog.Step, status godog.StepResultStatus, err error) (context.Context, error) {
		state, ok := ctx.Value(teamCityScenarioStateKey{}).(*teamCityScenarioState)
		if !ok {
			return ctx, nil
		}
		recordTeamCityStepResult(state, step, status, err)
		return ctx, nil
	})

	sc.After(func(ctx context.Context, _ *godog.Scenario, err error) (context.Context, error) {
		state, ok := ctx.Value(teamCityScenarioStateKey{}).(*teamCityScenarioState)
		if !ok {
			return ctx, nil
		}

		if err != nil {
			if errors.Is(err, godog.ErrSkip) {
				if state.result != teamCityScenarioFailed {
					state.result = teamCityScenarioSkipped
					if state.message == "" {
						state.message = "scenario skipped"
					}
				}
			} else {
				state.result = teamCityScenarioFailed
				state.message = err.Error()
			}
		}

		reporter.TestStarted(state.name, state.flowID)
		switch state.result {
		case teamCityScenarioFailed:
			reporter.TestFailed(state.name, state.message, state.flowID)
		case teamCityScenarioSkipped:
			reporter.TestIgnored(state.name, state.message, state.flowID)
		}
		reporter.TestFinished(state.name, state.flowID, reporter.Now().Sub(state.startedAt))
		return ctx, nil
	})
}

func recordTeamCityStepResult(state *teamCityScenarioState, step *godog.Step, status godog.StepResultStatus, err error) {
	if state.result == teamCityScenarioFailed {
		return
	}

	switch status {
	case godog.StepFailed, godog.StepUndefined, godog.StepPending, godog.StepAmbiguous:
		state.result = teamCityScenarioFailed
		state.message = teamCityStepMessage(step, status, err)
	case godog.StepSkipped:
		state.result = teamCityScenarioSkipped
		state.message = fmt.Sprintf("scenario skipped at step: %s", step.Text)
	default:
		if errors.Is(err, godog.ErrSkip) {
			state.result = teamCityScenarioSkipped
			state.message = fmt.Sprintf("scenario skipped at step: %s", step.Text)
		} else if err != nil {
			state.result = teamCityScenarioFailed
			state.message = err.Error()
		}
	}
}

func teamCityStepMessage(step *godog.Step, status godog.StepResultStatus, err error) string {
	if err != nil {
		return err.Error()
	}
	return fmt.Sprintf("step %q finished with status %s", step.Text, status)
}
