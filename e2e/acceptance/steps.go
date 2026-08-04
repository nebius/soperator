package acceptance

import (
	"github.com/cucumber/godog"

	"nebius.ai/soperator-e2e/acceptance/framework"
	"nebius.ai/soperator-e2e/acceptance/internal/sharedsteps"
)

// StepRegistrar registers step definitions and scenario hooks for a suite.
type StepRegistrar func(*godog.ScenarioContext, *framework.ClusterState, framework.Exec)

// SharedStepRegistrar returns a registrar for the shared Soperator step definitions.
func SharedStepRegistrar() StepRegistrar {
	return RegisterSharedSteps
}

// RegisterSharedSteps registers the shared Soperator step definitions.
func RegisterSharedSteps(sc *godog.ScenarioContext, state *framework.ClusterState, exec framework.Exec) {
	sharedsteps.RegisterAll(sc, state, exec)
}
