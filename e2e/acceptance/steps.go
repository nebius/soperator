package acceptance

import (
	"errors"

	"github.com/cucumber/godog"

	"github.com/nebius/soperator/e2e/acceptance/framework"
	"github.com/nebius/soperator/e2e/acceptance/internal/sharedsteps"
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
	sc.Step(`^the TeamCity reporting canary passes$`, func() error { return nil })
	sc.Step(`^the TeamCity reporting canary fails$`, func() error {
		return errors.New("SCHED-2392 controlled TeamCity reporting failure")
	})
}
