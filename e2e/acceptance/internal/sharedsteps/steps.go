package sharedsteps

import (
	"context"

	"github.com/cucumber/godog"

	"nebius.ai/soperator-e2e/acceptance/framework"
)

type scenarioScopedStepFamily interface {
	RegisterSteps(sc *godog.ScenarioContext)
	// CleanupAndReset runs before and after every scenario that uses the shared
	// step registrar. It must be cheap when the family has no tracked state.
	CleanupAndReset(ctx context.Context)
}

// RegisterAll registers every shared Soperator step family.
func RegisterAll(sc *godog.ScenarioContext, state *framework.ClusterState, exec framework.Exec) {
	slurm := framework.NewSlurmClient(exec)
	for _, family := range []scenarioScopedStepFamily{
		NewClusterCreation(state, exec),
		NewObservability(exec),
		NewInternalSSH(exec, slurm),
		NewPackageInstallation(exec, slurm),
		NewNodeReplacement(exec, slurm),
		NewDockerContainers(exec, slurm),
		NewEnrootContainers(exec, slurm),
		NewTopology(state, exec),
		NewPassiveChecks(exec, slurm),
		NewActiveChecks(state, exec, slurm),
		NewSystemChecks(exec, slurm),
	} {
		registerScenarioScoped(sc, family)
	}
}

func registerScenarioScoped(sc *godog.ScenarioContext, family scenarioScopedStepFamily) {
	sc.Before(func(ctx context.Context, scenario *godog.Scenario) (context.Context, error) {
		family.CleanupAndReset(context.Background())
		return ctx, nil
	})
	sc.After(func(ctx context.Context, scenario *godog.Scenario, err error) (context.Context, error) {
		family.CleanupAndReset(context.Background())
		return ctx, nil
	})
	family.RegisterSteps(sc)
}
