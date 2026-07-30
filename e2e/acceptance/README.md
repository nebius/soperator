# Acceptance Tests

This package contains the reusable Soperator acceptance runtime: embedded
Godog feature files, shared step definitions, and a runner for executing those
checks against an existing Soperator cluster.

Cluster lifecycle is intentionally outside this package. Create, select,
update, and delete the target cluster in the caller, then pass Kubernetes
access to the runner through a kube context.

> !!! WARNING !!!
> These tests mutate the target cluster. Run them only against a dev cluster
> or another environment that is safe to change.
> !!! WARNING !!!

## Standalone CLI

Build the standalone acceptance binary from the repository root:

```bash
go build -o bin/acceptance ./e2e/cmd/acceptance
```

Run the shared suite against an existing cluster:

```bash
bin/acceptance --kubectl-context <dev-context>
```

All flags:

- `--kubectl-context`: required. All local kubectl calls use this context.
- `--slurm-cluster-name`: optional, defaults to `soperator`.
- `--run-unstable`: optional, defaults to `false`; when false, scenarios tagged
  `@unstable` are excluded.
- `--scenario`: optional. Runs only the scenario at the provided feature path
  and line, for example `features/internal_ssh.feature:2`. May be repeated.
- `--report-dir`: optional. When set, the runner writes Cucumber and JUnit
  reports into that directory.

GPU scenarios are selected automatically. If no GPU workers are discovered,
scenarios tagged `@gpu` are excluded.

CPU scenarios are selected automatically. If no CPU workers are discovered,
scenarios tagged `@cpu` are excluded.

For focused manual runs on a dev cluster, pass the scenario location:

```bash
bin/acceptance --kubectl-context <dev-context> --scenario features/internal_ssh.feature:2
```

The `--scenario` flag is for local/manual investigation only. The GitHub
Actions e2e workflow does not pass it.

Note: The node replacement scenario uses the local `nebius` CLI to check
instance removal.

## Reusable Runner API

Use `nebius.ai/slurm-operator/e2e/acceptance` from another Go runner when you
want to reuse the shared Soperator scenarios for a different Soperator cluster
type.

```go
package main

import (
	"context"
	"log"

	"nebius.ai/slurm-operator/e2e/acceptance"
	"nebius.ai/slurm-operator/e2e/acceptance/framework"
)

func run(ctx context.Context, kubectlContext string) error {
	features := acceptance.SharedFeatureSource()
	features.Paths = []string{
		"features/internal_ssh.feature:2",
		"features/topology.feature:2",
	}

	runner, err := acceptance.NewRunner(acceptance.Options{
		SuiteName:                 "custom-soperator-acceptance",
		KubectlContext:            kubectlContext,
		SlurmClusterName:          "soperator",
		Features:                  features,
		Tags:                      "~@unstable",
		ExcludeMissingWorkerKinds: true,
		State:                     &framework.ClusterState{},
		StepRegistrars:            []acceptance.StepRegistrar{acceptance.SharedStepRegistrar()},
	})
	if err != nil {
		return err
	}
	return runner.Run(ctx)
}

func main() {
	if err := run(context.Background(), "dev-context"); err != nil {
		log.Fatal(err)
	}
}
```

The caller controls scenario selection through `FeatureSource.Paths` and Godog
tag filtering through `Options.Tags`. Additional runner-specific steps can be
registered by appending more `StepRegistrar` values.

The shared steps currently assume the Soperator namespace is `soperator`.
