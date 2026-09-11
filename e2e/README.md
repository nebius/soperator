# Acceptance Tests

This module contains the reusable Soperator acceptance runtime: embedded Godog
feature files, shared step definitions, and a runner for executing checks
against an existing Soperator cluster. It supports open-source Soperator
validation and external runners that reuse public Soperator scenarios against
another Soperator-based environment.

Cluster lifecycle is intentionally outside this module. Create, select, update,
and delete the target cluster in the caller, then pass Kubernetes access to the
runner through a kube context.

## Standalone CLI

> !!! WARNING !!!
> These tests mutate the target cluster. Run them only against a dev cluster
> or another environment that is safe to change.
> !!! WARNING !!!

Build the standalone acceptance binary from the Soperator repository root:

```bash
go -C e2e build -o ../bin/acceptance ./cmd/acceptance
```

Run the shared suite against an existing cluster:

```bash
bin/acceptance run \
  --kubectl-context <dev-context> \
  --output-dir e2e-artifacts/acceptance
```

The target Soperator version controls which version-tagged scenarios are run.
The CLI resolves it in this order:

1. `--soperator-version`
2. Flux HelmRelease discovery through the provided kubectl context

Use `--soperator-version` for a manual override, for example
`--soperator-version 5.0.0`. If the flag is omitted, the CLI uses kubectl to
discover the version from the target cluster.

Flux discovery reads the standard Soperator HelmRelease
`flux-system/flux-system-soperator-fluxcd-soperator`. It uses
`status.lastAppliedRevision` when present and falls back to
`status.lastAttemptedRevision` when the applied revision is empty. If discovery
cannot resolve a version, the CLI fails before running scenarios and asks for
`--soperator-version`.

`run` flags:

- `--kubectl-context`: required. All local kubectl calls use this context.
- `--slurm-cluster-name`: optional, defaults to `soperator`.
- `--soperator-version`: optional target Soperator version. Full
  `major.minor.patch` is required, with optional suffix allowed for deployed
  versions, for example `4.1.5-reb85d0e5`.
- `--run-unstable`: optional, defaults to `false`; when false, scenarios tagged
  `@unstable` are excluded.
- `--run-essential`: optional, defaults to `false`; when true, only scenarios
  tagged `@essential` are run.
- `--scenario`: optional. Runs all compatible scenarios in the provided feature
  file, or the single scenario at an exact `Scenario:` line, for example
  `features/internal_ssh.feature:3`. May be repeated.
- `--output-dir`: required. The runner writes Cucumber and JUnit reports plus
  per-scenario artifacts into this directory.

CPU/GPU scenarios are selected by tags like other scenarios. Steps that need a
specific worker kind query live Slurm and NodeSet state at scenario time. They
skip when the required capacity is not configured, but fail when it is
configured and too few workers are usable. Scenarios that cover all workers
also fail on partial degradation.

For focused manual runs on a dev cluster, pass the scenario location:

```bash
bin/acceptance run \
  --kubectl-context <dev-context> \
  --output-dir e2e-artifacts/acceptance \
  --scenario features/internal_ssh.feature:3
```

In the GitHub Actions **E2E test soperator** workflow, use the optional
`scenarios` field to select the same feature paths or exact scenario lines.
Separate multiple selectors with commas, semicolons, or whitespace, for example
`features/internal_ssh.feature:3, features/observability.feature:3`.
Separators can be mixed; repeated separators and surrounding whitespace are
ignored. Feature paths must not contain these separators.
Each selector is passed as a separate `--scenario` argument. Leave the field
empty to run all compatible scenarios subject to the tag filters below.
Paths refer to features in the selected **E2E repo branch**.

On a PR, use `/e2e [terraform-branch] --scenarios="<selectors>"` or
`/e2e-essential [terraform-branch] --scenarios="<selectors>"`. Use the same
separators as the workflow input. Double quotes are optional for lists without
whitespace; use double quotes when the list contains spaces:

```text
/e2e --scenarios="features/internal_ssh.feature:3, features/observability.feature"
/e2e my-terraform-branch --scenarios=features/internal_ssh.feature:3
```

The Terraform branch defaults to `main`. Omit `--scenarios` or pass an empty
list to keep the default scenario selection. The same tag filters apply.
The PR branch must contain a workflow declaring the `scenarios` input, even
when the list is empty.

`--run-essential` composes with the other selectors. By default, unstable
essential scenarios are still excluded; pass `--run-unstable` to include them.
When combined with `--scenario`, the selected scenario must also carry the
`@essential` tag.

The selected-worker node replacement scenario is both essential and unstable,
so it runs only when both `--run-essential` and `--run-unstable` are set.

Note: The node replacement scenario uses the local `nebius` CLI to check
instance removal.

Collect a reusable cluster snapshot before or after acceptance:

```bash
bin/acceptance collect-artifacts \
  --kubectl-context <dev-context> \
  --slurm-cluster-name soperator \
  --soperator-version 5.0.0 \
  --output-dir e2e-artifacts/snapshots/before-destroy
```

The common snapshot contains Kubernetes, Soperator, redacted FluxCD, Slurm,
and jail diagnostics. Add `--nebius-project-id <project-id>` to include Managed
Kubernetes clusters and node groups. When that optional flag is omitted, the
`mk8s` collector creates no files and does not invoke the Nebius CLI.
If Terraform fails before a Kubernetes context is available, omit
`--kubectl-context` and provide `--nebius-project-id`; the command will skip the
Kubernetes-bound collectors and still capture Managed Kubernetes state.

Collectors are dependency-bound and reusable by another E2E runtime:

```go
type Collector interface {
	Name() string
	Collect(ctx context.Context, destination string) error
}
```

Managed E2E can compose the common constructors with collectors built from its
own runtime dependencies. `artifacts.CollectAll` runs all collectors, keeps
successful output when another collector fails, and records the failure in the
affected collector directory.

## Reusable Runner API

Use `github.com/nebius/soperator/e2e/acceptance` from another Go runner when you
want to reuse the shared Soperator scenarios for a different Soperator cluster
type.

```go
suite := acceptance.SoperatorSuite("5.0.0")
suite.Source.Paths = []string{"features/internal_ssh.feature:3"}
suite.Tags = "~@custom_only"
suite.ExcludeUnstable = false

runner, err := acceptance.NewRunner(acceptance.RunnerConfig{
	KubectlContext:         kubectlContext,
	SlurmClusterName:       "soperator",
	TargetSoperatorVersion: "5.0.0",
	OutputDir:              "e2e-artifacts/acceptance",
	Suites:                 []acceptance.SuiteConfig{suite},
})
```

`KubectlContext` is required and is the normal way to point the runner at the
target cluster. `SlurmClusterName` defaults to `soperator` when omitted.
`TargetSoperatorVersion` is normalized once and stored in static
`framework.ClusterInfo`. The caller controls scenario selection through
`SuiteConfig.Source.Paths` and Godog tag filtering through `SuiteConfig.Tags`.
When `OutputDir` is set, each scenario receives matching runner and jail paths
through `framework.ScenarioArtifacts(ctx)`. Product-specific steps can write
their own files directly to those paths without registering a collector.

`acceptance.SoperatorSuite(targetSoperatorVersion)` embeds the public Soperator
feature files, registers public Soperator steps, enables the default shared
filters, and filters scenarios by `@soperator_version_...`. Custom runners can
add their own suites. Each suite is a separate Godog suite with its own feature
source, tag expression, step registrars, version axes, and report files. Suite
names are required, must be unique, and must match `^[A-Za-z0-9._-]+$`; reports
are written as `<suite>.cucumber.json` and `<suite>.junit.xml`.

Step registrars receive static `framework.ClusterInfo` and the shared
`framework.Runtime` interface:

```go
func registerProductSteps(sc *godog.ScenarioContext, info *framework.ClusterInfo, runtime framework.Runtime) {
	productSteps := newProductSteps(info, runtime)
	productSteps.RegisterSteps(sc)
}
```

Suites with no version axes are passed through unchanged. Suites with axes are
filtered before Godog starts:

```go
productSuite := acceptance.SuiteConfig{
	Name:   "product",
	Source: productFeatures,
	Tags:   "@smoke && ~@slow",
	VersionAxes: []acceptance.ScenarioVersionAxis{
		acceptance.SoperatorVersionAxis("5.0.0"),
		acceptance.VersionAxis("@product_version_", "1.8.2"),
	},
	StepRegistrars: []acceptance.StepRegistrar{
		acceptance.SharedStepRegistrar(),
		registerProductSteps,
	},
	ExcludeUnstable: true,
}
```

Every scenario must have exactly one `@soperator_version_...` tag. Put the tag
on the scenario itself and use an explicit full patch-version constraint such as
`@soperator_version_>=5.0.0`. This tag means the scenario runs only when the
normalized target Soperator version satisfies the constraint. Shorthand forms
such as `@soperator_version_>=5.0` are intentionally rejected. Comma means AND
and `||` means OR, for example
`@soperator_version_>=4.1.0,<4.4.0||>=5.0.0,<6.0.0`.

The runner stores and uses a normalized target Soperator version for filtering.
Suffixes and build metadata are stripped before comparison, so
`4.1.5-reb85d0e5` is stored and compared as `4.1.5`.

Custom suites that also depend on Soperator behavior should include both the
custom product axis and the Soperator axis. Custom scenarios can reuse public
Soperator step definitions by registering `acceptance.SharedStepRegistrar()` in
that suite. All suites are configured before the run starts. Scenario-local
fields such as selected workers, Slurm job IDs, and ActiveCheck job IDs are
owned by the shared step objects and reset before and after every shared
scenario.

## Static Suites

External runners should build all suites before constructing the runner:

```go
productSuite := acceptance.SuiteConfig{
	Name: "product",
	Source: productFeatures,
	VersionAxes: []acceptance.ScenarioVersionAxis{
		acceptance.SoperatorVersionAxis("5.0.0"),
		acceptance.VersionAxis("@product_version_", "1.8.2"),
	},
	StepRegistrars: []acceptance.StepRegistrar{
		registerProductSteps,
	},
}

runner, err := acceptance.NewRunner(acceptance.RunnerConfig{
	KubectlContext:         kubectlContext,
	TargetSoperatorVersion: "5.0.0",
	Suites: []acceptance.SuiteConfig{
		acceptance.SoperatorSuite("5.0.0"),
		productSuite,
	},
})
```

The runner does not perform run-level cluster discovery. Per-scenario checks
that need fresh cluster state should query Kubernetes or Slurm through
`framework.KubectlClient`, `framework.SlurmClient`, or a caller-owned client.

## Framework Helpers

External runners can use `github.com/nebius/soperator/e2e/acceptance/framework` for
common Kubernetes, Slurm, and worker primitives:

- `Exec`: command execution interface.
- `Runtime`: `Exec` plus polling and logging, used by shared steps and external suites.
- `ClusterInfo`: static SlurmCluster name and target Soperator version.
- `KubectlClient`, including `GetJSON`, `NodeSets`, `WorkerPods`, and
  `WorkerPodForSlurmNode`.
- `SlurmClient`, including `NodeInfo`, `NodeInfoOnce`, `JobInfo`,
  `MainPartitionNodeNames`, `WaitForJobRunning`, and `WaitForJobGone`.
- `WorkerSelector`, `WorkerSnapshot`, and `WorkerInfo` for retaining live Slurm
  state and configured NodeSet capacity, plus
  `PickWorkers`/`PickCPUWorkers`/`PickGPUWorkers` for selecting usable workers.
- `SkipIfInsufficientWorkers` for mapping insufficient-worker selections to
  skipped Godog scenarios.
- `SbatchJob`, `SbatchOptions`, `ShellQuote`, `BashLC`, and `WorkerNames`.
- `WaitForWithJobAlive` and `AnnotateWithJobLog` for polling while a Slurm job
  is still alive and attaching useful job logs to failures.

Concrete public Soperator step structs are intentionally internal. Reuse shared
steps through `acceptance.SharedStepRegistrar()` and add caller-specific steps
through additional `StepRegistrar` values.

## Package Layout

- `acceptance`: public runner API, embedded feature files, suite configs,
  and shared step registrar.
- `acceptance/framework`: public helper types for command execution, static
  cluster info, Kubernetes JSON queries, Slurm jobs, worker selection, retries,
  and shell quoting.
- `acceptance/internal/sharedsteps`: concrete public Soperator step
  implementations and scenario cleanup/reset.
- `acceptance/internal/kubeobjects`: Kubernetes object shapes used by shared
  framework helpers and steps.
- `acceptance/internal/reports`: Godog report format construction.
- `cmd/acceptance`: standalone runner for an already deployed cluster.

## Synchronization

The acceptance implementation mirrors the standalone `soperator-e2e`
repository. Keep shared source and tests synchronized, while treating this
README, module manifests, and repository-level metadata as location-specific.

GitHub Actions keeps two refs separate:

1. Soperator/workflow ref: the Soperator repository ref used to run the E2E
   code and select the latest successful Soperator build artifact to deploy.
2. Terraform repo ref: the `nebius-solution-library` ref used for cluster
   creation. It defaults to the Soperator/workflow ref, but can be overridden
   independently.

The workflow reads the target Soperator version from the selected build artifact
and uses it for Terraform deployment and scenario filtering. When new tests or
behavior are added, the scenarios must be tagged with the full version lower
bound where that behavior exists, for example `@soperator_version_>=5.0.0`.
