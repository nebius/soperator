# AGENTS.md

The role of this file is to describe common mistakes and confusion points that agents might encounter as they work
in this project.

## Architecture

Kubernetes operator for Slurm, built with kubebuilder. Manages Slurm cluster lifecycle on Kubernetes via custom resources.

### Directory map

- `api/` — CRD types (v1: SlurmCluster; v1alpha1: NodeSet, ActiveCheck, JailedConfig, NodeConfigurator, NodeSetPowerState)
- `cmd/` — binary entry points (main operator, e2e, exporter, rebooter, sconfigcontroller, soperatorchecks, powermanager)
- `internal/controller/` — reconcilers (clustercontroller, nodesetcontroller, sconfigcontroller, topologyconfcontroller, nodeconfigurator, soperatorchecks)
- `internal/render/` — Kubernetes manifest generation per component (common, controller, worker, login, accounting, rest, exporter, etc.)
- `internal/values/` — typed configuration objects fed into render
- `internal/slurmapi/` — Slurm REST API client
- `internal/webhook/` — validating/mutating webhooks (v1, v1alpha1)
- `internal/exporter/` — Prometheus metrics collector for Slurm
- `internal/e2e/` — E2E test framework code
- `helm/` — Helm charts (soperator, slurm-cluster, nodesets, soperator-crds, soperator-activechecks, soperator-fluxcd, nfs-server, etc.); chart tests live in `helm/<chart>/tests/`
- `config/` — raw K8s manifests (CRDs, RBAC, webhooks, cert-manager, samples)
- `docs/` — documentation (architecture, features, active-checks, local-development, release-flow, etc.)
- `.github/workflows/` — CI: e2e_test, one_job, nightly_build, github_release, etc.
- `test/integration/` — Helm deployment integration tests
- `images/` — Dockerfiles for component images
- `ansible/` — operational playbooks
- `fluxcd/` — FluxCD configuration. This is legacy configuration and will be removed in the future. Please ignore it when making changes.


## Style Guide

In Go Code:
- Follow Uber Go Style
- Start error messages with a lowercase letter (unless beginning with a proper noun or acronym); log messages, on the other hand, should be capitalized
- Start error messages with an infinitive, skip parts like "failed to", "cannot", in order not to repeat them when chaining error messages
- Use var array initialization, e.g. `var env []EnvVar`, not `env := []EnvVar{}`

In any programming language:
- Do not write obvious comments in the code, like explaining every line of code, comment only interfaces and tricky parts

In GitHub Workflows:
- Use `shell: bash` for shell steps since it adds pipefail for better error handling.
- For actions, prefer SHA pinning with version comment, e.g., `user/action@sha256hash # v1.2.3`
- Do not use booleans for input variables, use strings instead: bools require complicated expressions
- `one_job.yml` runs on `pull_request_target`, so the definition from main serves PRs into every branch. When changing it, keep its jobs working against the current release branches' checkouts too — their repo layout may differ from main's. The current release branches are listed in `e2e_release_branches` in `.github/branch-config.yaml` on main.

In Documentation:
- Do not overuse *bold*

## Local development

- Run `make sync-version-from-scratch` after modifying helm chart values/templates to regenerate manifests and sync versions (CI will fail otherwise).
