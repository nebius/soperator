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
func RegisterAll(sc *godog.ScenarioContext, info *framework.ClusterInfo, runtime framework.Runtime) {
	slurm := framework.NewSlurmClient(runtime)
	kubectl := framework.NewKubectlClient(runtime)
	selector := framework.NewWorkerSelector(kubectl, slurm, info.SlurmClusterName)
	for _, family := range []scenarioScopedStepFamily{
		NewClusterCreation(info, runtime, kubectl, selector),
		NewObservability(kubectl),
		NewInternalSSH(runtime, selector),
		NewPackageInstallation(runtime, selector),
		NewNodeReplacement(runtime, slurm, selector),
		NewDockerContainers(runtime, slurm, selector),
		NewEnrootContainers(runtime, slurm, selector),
		NewTopology(runtime, selector),
		NewPassiveChecks(info, runtime, slurm, selector),
		NewActiveChecks(info, runtime, slurm, kubectl, selector),
		NewSystemChecks(runtime, slurm, kubectl, selector),
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
