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
go -C e2e build -o ../bin/acceptance ./cmd/acceptance
```

Run the shared suite against an existing cluster:

```bash
bin/acceptance --kubectl-context <dev-context>
```

The target Soperator version controls which version-tagged scenarios are run.
The CLI resolves it in this order:

1. `--soperator-version`
2. Flux HelmRelease discovery through the provided kubectl context

Use `--soperator-version` for a manual override, for example
`--soperator-version 4.2.0`. If the flag is omitted, the CLI uses kubectl to
discover the version from the target cluster.

Flux discovery reads `status.lastAttemptedRevision` from the standard Soperator
HelmRelease: `flux-system/flux-system-soperator-fluxcd-soperator`. If
discovery cannot do that, the CLI fails before running scenarios and asks for
`--soperator-version`.

All flags:

- `--kubectl-context`: required. All local kubectl calls use this context.
- `--slurm-cluster-name`: optional, defaults to `soperator`.
- `--soperator-version`: optional target Soperator version. Full
  `major.minor.patch` is required, with optional suffix allowed for deployed
  versions, for example `4.1.5-reb85d0e5`.
- `--run-unstable`: optional, defaults to `false`; when false, scenarios tagged
  `@unstable` are excluded.
- `--scenario`: optional. Runs all compatible scenarios in the provided feature
  file, or the single scenario at an exact `Scenario:` line, for example
  `features/internal_ssh.feature:3`. May be repeated.
- `--report-dir`: optional. When set, the runner writes Cucumber and JUnit
  reports into that directory.

GPU scenarios are selected automatically. If no GPU workers are discovered,
scenarios tagged `@gpu` are excluded.

CPU scenarios are selected automatically. If no CPU workers are discovered,
scenarios tagged `@cpu` are excluded.

For focused manual runs on a dev cluster, pass the scenario location:

```bash
bin/acceptance --kubectl-context <dev-context> --scenario features/internal_ssh.feature:3
```

The `--scenario` flag is for local/manual investigation only. The GitHub
Actions e2e workflow does not pass it.

Note: The node replacement scenario uses the local `nebius` CLI to check
instance removal.

## Reusable Runner API

Use `nebius.ai/soperator-e2e/acceptance` from another Go runner when you
want to reuse the shared Soperator scenarios for a different Soperator cluster
type.

```go
package main

import (
	"context"
	"log"

	"nebius.ai/soperator-e2e/acceptance"
	"nebius.ai/soperator-e2e/acceptance/framework"
)

func run(ctx context.Context, kubectlContext string) error {
	features := acceptance.SharedFeatureSource()
	features.Paths = []string{
		"features/internal_ssh.feature:3",
		"features/topology.feature:3",
	}

	runner, err := acceptance.NewRunner(acceptance.Options{
		SuiteName:                 "custom-soperator-acceptance",
		KubectlContext:            kubectlContext,
		SlurmClusterName:          "soperator",
		TargetSoperatorVersion:    "4.2.0",
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
tag filtering through `Options.Tags`. The runner always pre-filters scenarios
by `TargetSoperatorVersion` before invoking Godog.

Every scenario must have exactly one `@soperator_version_...` tag. Version tags
must be on the scenario itself and use full patch versions only, for example
`@soperator_version_>=4.2.0`; shorthand forms such as
`@soperator_version_>=4.2` are intentionally rejected. Comma means AND and `||`
means OR, for example
`@soperator_version_>=4.1.0,<4.4.0||>=5.0.0,<6.0.0`.

The runner stores and uses a normalized target version for filtering. Suffixes
and build metadata are stripped before comparison, so `4.1.5-reb85d0e5` is
stored and compared as `4.1.5`.

GitHub Actions keeps two refs separate:

1. Soperator/workflow ref: the Soperator repository ref used to run the E2E
   code and select the latest successful Soperator build artifact to deploy.
2. Terraform repo ref: the `nebius-solution-library` ref used for cluster
   creation. It defaults to the Soperator/workflow ref, but can be overridden
   independently.

The workflow reads the target Soperator version from the selected build artifact
and uses it for Terraform deployment and scenario filtering. When new tests or
behavior are added, the scenarios must be tagged with the full version lower
bound where that behavior exists, for example `@soperator_version_>=4.2.0`.

The shared steps currently assume the Soperator namespace is `soperator`.
