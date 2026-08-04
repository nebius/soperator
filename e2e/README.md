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
`--soperator-version 5.0.0`. If the flag is omitted, the CLI uses kubectl to
discover the version from the target cluster.

Flux discovery reads the standard Soperator HelmRelease
`flux-system/flux-system-soperator-fluxcd-soperator`. It uses
`status.lastAppliedRevision` when present and falls back to
`status.lastAttemptedRevision` when the applied revision is empty. If discovery
cannot resolve a version, the CLI fails before running scenarios and asks for
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
suite := acceptance.SoperatorSuite("5.0.0")
suite.Source.Paths = []string{"features/internal_ssh.feature:3"}
suite.Tags = "~@custom_only"
suite.FilterOptions.ExcludeUnstable = false
suite.FilterOptions.ExcludeMissingWorkerKinds = true

runner, err := acceptance.NewRunner(acceptance.Options{
	KubectlContext:         kubectlContext,
	SlurmClusterName:       "soperator",
	TargetSoperatorVersion: "5.0.0",
	Suites:                 []acceptance.SuiteConfig{suite},
	State:                  &framework.ClusterState{},
})
```

`KubectlContext` is required and is the normal way to point the runner at the
target cluster. `SlurmClusterName` defaults to `soperator` when omitted. The
caller controls scenario selection through `SuiteConfig.Source.Paths` and Godog
tag filtering through `SuiteConfig.Tags`.

`acceptance.SoperatorSuite(targetSoperatorVersion)` embeds the public Soperator
feature files, registers public Soperator steps, enables the default shared
filters, and filters scenarios by `@soperator_version_...`. Custom runners can
add their own suites. Each suite is a separate Godog suite with its own feature
source, tag expression, step registrars, version axes, and report files. Suite
names are required, must be unique, and must match `^[A-Za-z0-9._-]+$`; reports
are written as `<suite>.cucumber.json` and `<suite>.junit.xml`.

Suites with no version axes are passed through unchanged. Suites with axes are
filtered before Godog starts:

```go
productSuite := acceptance.SuiteConfig{
	Name:   "product",
	Source: productFeatures,
	Tags:   "@smoke && ~@slow",
	VersionAxes: []versionfilter.Axis{
		acceptance.SoperatorVersionAxis("5.0.0"),
		{TagPrefix: "@product_version_", TargetVersion: "1.8.2"},
	},
	StepRegistrars: []acceptance.StepRegistrar{
		acceptance.SharedStepRegistrar(),
		registerProductSteps,
	},
	FilterOptions: acceptance.SuiteFilterOptions{ExcludeUnstable: true},
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
that suite. All suites share one `framework.ClusterState`, while scenario-local
fields such as selected workers, Slurm job IDs, and ActiveCheck job IDs are
owned by the shared step objects and reset before and after every shared
scenario.

## Discovery Hooks

The shared runner always performs base Soperator discovery before running
scenarios. It verifies the controller/login pods, discovers NodeSets, discovers
Slurm workers, classifies CPU/GPU workers, and stores that data in
`framework.ClusterState`.

External runners can add discovery hooks for caller-specific state. Hooks share
the same `framework.ClusterState` and `framework.Exec` as the suites:

```go
discoverProduct := func(ctx context.Context, state *framework.ClusterState, exec framework.Exec) error {
	// discover product-specific cluster data and store it in caller-owned state
	return nil
}

runner, err := acceptance.NewRunner(acceptance.Options{
	KubectlContext:         kubectlContext,
	TargetSoperatorVersion: "5.0.0",
	Suites: []acceptance.SuiteConfig{
		acceptance.SoperatorSuite("5.0.0"),
		productSuite,
	},
	DiscoveryHooks: []acceptance.DiscoveryHook{discoverProduct},
	State:          &framework.ClusterState{},
})
```

Hooks run once after base discovery and before feature selection/Godog
execution. Per-scenario checks that need fresh state should still use Godog
`Before` hooks inside the caller's own step registrar.

## Framework Helpers

External runners can use `nebius.ai/soperator-e2e/acceptance/framework` for
common Kubernetes, Slurm, and worker primitives:

- `Exec`: command execution interface used by shared steps and hooks.
- `ClusterState`, `WorkerRef`, and discovered worker/node-set fields.
- `SlurmClient`, including `NodeInfo`, `NodeInfoOnce`, `JobInfo`,
  `WaitForJobRunning`, `WaitForJobGone`, and worker selection helpers.
- `SbatchJob`, `SbatchOptions`, `ShellQuote`, `BashLC`, `WorkerNames`.
- `WaitForWithJobAlive` and `AnnotateWithJobLog` for polling while a Slurm job
  is still alive and attaching useful job logs to failures.

Concrete public Soperator step structs are intentionally internal. Reuse shared
steps through `acceptance.SharedStepRegistrar()` and add caller-specific steps
through additional `StepRegistrar` values.

## Package Layout

- `acceptance`: public runner API, embedded feature files, suite configs,
  discovery hooks, and shared step registrar.
- `acceptance/framework`: public helper types for command execution, cluster
  state, Slurm jobs, workers, retries, and shell quoting.
- `acceptance/internal/sharedsteps`: concrete public Soperator step
  implementations and scenario cleanup/reset.
- `acceptance/internal/discovery`: base Soperator cluster discovery.
- `acceptance/internal/kubeobjects`: Kubernetes object shapes used by shared
  discovery and steps.
- `acceptance/internal/reports`: Godog report format construction.
- `cmd/acceptance`: standalone runner for an already deployed cluster.

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

The shared steps currently assume the Soperator namespace is `soperator`.
