package acceptance

import (
	"github.com/cucumber/godog"

	"nebius.ai/soperator-e2e/acceptance/framework"
	"nebius.ai/soperator-e2e/acceptance/internal/sharedsteps"
)

// StepRegistrar registers step definitions and scenario hooks for a suite.
type StepRegistrar func(*godog.ScenarioContext, *framework.ClusterInfo, framework.Runtime)

// SharedStepRegistrar returns a registrar for the shared Soperator step definitions.
func SharedStepRegistrar() StepRegistrar {
	return RegisterSharedSteps
}

// RegisterSharedSteps registers the shared Soperator step definitions.
func RegisterSharedSteps(sc *godog.ScenarioContext, info *framework.ClusterInfo, runtime framework.Runtime) {
	sharedsteps.RegisterAll(sc, info, runtime)
}
