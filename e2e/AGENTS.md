# E2E scenario development

These instructions apply to the standalone E2E module under `e2e/`. Read
`README.md` before changing the runner, framework, features, or shared steps.

## Agree on the scenario before implementing it

New scenario development starts with feature context supplied by the user. The
context may be a description, code, a pull request, a Jira ticket, a Confluence
page, a Slack thread, or another source. Inspect the supplied sources and the
relevant Soperator code before proposing a test.

Before proposing an implementation, search the E2E module for the same
component, Kubernetes resource types, label selectors, commands, CR fields,
polling logic, and validation behavior. Identify code that can be reused, code
that should be extracted for another caller, and any duplication that should
intentionally remain local.

Gather enough information to answer:

- What user-visible behavior is being introduced or changed?
- Which Soperator versions have the old and new behavior?
- What cluster configuration and capacity does the scenario require, and for
  each unmet prerequisite should the scenario skip or fail?
- Which action should the scenario perform, and what observations prove that it
  worked?
- Should the scenario be part of the essential suite?
- What cluster state will the scenario mutate? Which mutations must be restored?
- What timing, stability, and diagnostic requirements are known?

If material requirements are missing or conflicting, ask the user instead of
inventing them.

Before editing code, propose a scenario design for user approval. Include:

- the intended `.feature` changes, preferably with draft Gherkin;
- the version, essential, unstable, and descriptive tag decisions;
- the skip-versus-fail decisions and prerequisite checks;
- the existing implementations and helpers found during the reuse audit,
  including any small refactor needed before adding the scenario;
- the important implementation choices, helpers, polling, cleanup, and tests;
- the focused command and cluster shape needed for live validation.

Do not implement a new scenario until the user approves this design. If later
investigation materially changes it, explain the change and obtain approval
again.

## Scenario design

- Describe observable behavior in Gherkin. Keep kubectl, Slurm command syntax,
  parsing, retries, and other implementation details in Go steps.
- Keep scenarios independent. Do not rely on scenario execution order or state
  left by another scenario.
- Reuse an existing step only when its wording and semantics mean exactly the
  same thing. Prefer a new precise step to broadening an existing one until its
  behavior becomes surprising.
- Decide whether a scenario is essential during design, before implementation.
  `@essential` is for release-critical baseline coverage, not merely useful or
  important coverage. Adding or removing it requires updating the expectations
  in `acceptance/features_test.go`.
- Decide skip versus failure explicitly for every prerequisite. Return
  `godog.ErrSkip` only when a valid target cluster does not provide an optional
  prerequisite, such as GPU capacity or a particular topology. Fail when the
  prerequisite is configured but unhealthy or insufficient, when the scenario
  cannot determine cluster state, or when product behavior is wrong. Never use
  a skip to hide flakiness, a broken environment that should satisfy the test,
  or an unfinished implementation.

## Tags and versioned behavior

The shared feature suite has three tag forms that affect execution:

- exactly one scenario-level `@soperator_version_...` constraint;
- optional `@essential`;
- optional `@unstable`.

CPU, GPU, topology, and other descriptive tags are allowed, but no current
runner or workflow consumes them. Ask whether the user wants these tags and
follow their preference consistently for the scenario being developed. Never
present a descriptive tag as functional filtering. Regardless of whether such
tags are present, express prerequisites in the scenario and have its steps
inspect live cluster state, then skip or fail according to the rule above.

Use a full patch version in every version constraint, for example
`@soperator_version_>=5.1.0`. When behavior changes in a new version, do not
silently rewrite the old scenario and move its lower version bound. Preserve
the old behavior for supported older versions and add a separate scenario for
the new behavior, for example:

```gherkin
@soperator_version_>=5.0.0,<5.2.0
Scenario: Existing behavior
  ...

@soperator_version_>=5.2.0
Scenario: New behavior
  ...
```

Remove the old scenario only after the versions it covers are no longer
supported by this suite.

Use `@unstable` only when the scenario is known not to work reliably enough for
the default suite. A feature that should normally work is not unstable merely
because it is slow, destructive, or has strict prerequisites. There should
normally be a ticket queued to stabilize every unstable scenario. Add a `TODO`
next to the tag in the feature file that explains the current problem, links or
names the ticket when available, and states what must happen before the tag can
be removed.

## Step implementation

Feature files live in `acceptance/features`. Shared step families live in
`acceptance/internal/sharedsteps`. When adding a feature or family:

- add default-suite features to `acceptance.FeaturePaths`, or explicitly list a
  deliberately manual feature in `manualFeatures` in `features_test.go`;
- register new step families in `acceptance/internal/sharedsteps/steps.go`;
- unit-test parsing, validation, selection, and command construction separately
  from live-cluster behavior.

Use `framework.KubectlClient`, `framework.SlurmClient`, `WorkerSelector`, job
helpers, execution scopes, polling, shell quoting, and artifact paths whenever
they fit. When a new scenario becomes another caller of domain logic embedded
in an existing step family, extract that logic and migrate both callers instead
of copying it. Another caller is enough to justify extraction when the code
encodes resource names, selectors, discovery rules, or other domain knowledge
that could drift. Do not introduce an abstraction merely for incidental
syntactic duplication.

Place shared code at the narrowest layer that fits:

- Keep scenario-specific behavior in its step family.
- Put component-specific behavior shared by step families in an unexported
  helper under `acceptance/internal/sharedsteps`.
- Add operations to `acceptance/framework` clients when they are broadly
  reusable Kubernetes, Slurm, execution, polling, or selection primitives.

If a step needs a kubectl, `scontrol`, or similar operation, consider a typed
client method even when there is only one caller today if the operation has
plausible broad reuse or benefits from centralized parsing, errors, or retries.
Keep a simple one-off operation in the step when extracting it would only add
indirection. Do not expose component-specific behavior through the public
framework solely to remove local duplication.

Prefer native Go implementation over Bash. In particular, use typed Go parsing
for structured output and Go control flow for multi-step behavior. If shell is
the clearest way to perform a small remote probe, keep it small and focused.
Split a large shell program into independently named Go operations or several
small commands when that improves error reporting and testability. Do not build
an elaborate Go abstraction merely to eliminate a short, stable shell command.

Query Kubernetes and Slurm state through the provided runtime scopes. Do not
invoke local kubectl directly: `Runtime.Kubectl()` applies the explicit target
context. Use `Runtime.WaitFor` and the framework job wait helpers for eventual
state instead of fixed sleeps. Choose timeouts from expected system behavior
and make timeout errors carry the state needed for triage.

Keep step families stateless by default. Store state on the step-family struct
when it must be passed between steps, restored during cleanup, or when repeating
an expensive query would provide no fresher information. Do not track whether
an earlier step succeeded: Godog preserves step order and stops on failure.
Query the cluster again when the scenario depends on observing state changes or
freshness. Do not use package-global mutable state.

## Cleanup, artifacts, and diagnostics

Every shared step family implements `CleanupAndReset` and it runs before and
after each scenario. It must be cheap and idempotent. Always reset in-memory
scenario state. An empty `CleanupAndReset` is correct for a stateless family
that owns no external state requiring cleanup.

Clean up external state when leaving it behind could affect another scenario or
rerun, consume scarce or billable resources, leave a disruptive process or
job, or keep shared cluster configuration changed. Record the original value
before a mutation so it can be restored after partial execution.

Do not add complicated cleanup solely to remove harmless leftovers that cannot
affect later acceptance tests, reruns, cluster operation, or cost. In that case,
less code and fewer cleanup failure modes are preferable. When cleanup is
best-effort, log failures with enough resource identifiers to handle them
manually; cleanup must not replace the scenario's original failure.

If a scenario produces an artifact that should survive the run, such as a
report, profile, dump, or substantial command output, obtain its assigned paths
with `framework.ScenarioArtifacts(ctx)` and store it there. Use `RunnerDir` for
files produced by the local runner and `JailDir` for files produced inside the
jail. Temporary intermediate files may live elsewhere, but move or copy the
result into the scenario artifact directory before cleanup. Do not use a shared
fixed path for final artifacts or remove the assigned artifact directory from
`CleanupAndReset`.

Failure messages should identify the operation, relevant resource, expected
state, and observed state. Prefer existing helpers such as
`AnnotateWithJobLog` when they preserve the evidence needed to investigate a
CI-only failure.

## Validation

Run cheap validation first from the repository root:

```bash
go -C e2e test ./...
go -C e2e vet ./...
go -C e2e build -o ../bin/acceptance ./cmd/acceptance
```

Every new scenario must then be run by itself against a development cluster.
Acceptance scenarios can mutate or disrupt the cluster. Never infer that an
arbitrary kube context is safe: either the user runs the focused scenario, or
the user provides a context and explicitly confirms that it targets a dev/test
cluster that is safe to mutate.

When a confirmed context is available, use the exact scenario line:

```bash
bin/acceptance run \
  --kubectl-context <dev-context> \
  --output-dir e2e-artifacts/acceptance \
  --scenario features/<feature>.feature:<scenario-line>
```

If no safe cluster is available, stop after static validation and give the user
the exact focused command to run. Do not report the scenario as live-validated.

The acceptance implementation mirrors the standalone `soperator-e2e`
repository. Keep shared source and tests synchronized according to the current
repository workflow; do not assume that changing only one copy is sufficient.
