# Acceptance Tests On A Dev Cluster

Use the standalone acceptance binary to run the acceptance suite against an
existing dev Soperator cluster.

> !!! WARNING !!!
> These tests mutate the target cluster. Run them only against a dev cluster
> that is safe to change.
> !!! WARNING !!!

## Build

From the repository root:

```bash
go build -o bin/acceptance ./cmd/acceptance
```

## Run

Minimal manual run:

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

For focused manual runs on a dev cluster, pass the scenario location:

```bash
bin/acceptance --kubectl-context <dev-context> --scenario features/internal_ssh.feature:2
```

The `--scenario` flag is for local/manual investigation only. The GitHub
Actions e2e workflow does not pass it.

Note: The node replacement scenario uses the local `nebius` CLI to check instance removal.
