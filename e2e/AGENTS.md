# E2E scenario development

These instructions apply to the standalone E2E module under `e2e/`. Read
`README.md` before changing the runner, framework, features, or shared steps.

## Agree on the scenario before implementing it

New scenario development starts with feature context supplied by the user. The
context may be a description, code, a pull request, a Jira ticket, a Confluence
page, a Slack thread, or another source. Inspect the supplied sources and the
relevant Soperator code before proposing a test.

## Mandatory reuse audit

The reuse audit is a blocking part of scenario design. Do not propose a design
until it is complete.

For every capability the scenario needs:

1. Search all of `e2e/acceptance`, not only the target feature's step file.
   Include `internal/sharedsteps`, `framework`, tests, and feature files.
2. Search using:
   - domain concepts and synonyms;
   - Kubernetes kinds, CR fields, labels, commands, and paths;
   - exact significant literals;
   - analogous operations, such as worker selection, job submission, polling,
     SSH setup, cleanup, parsing, and diagnostics.
3. Read every plausible implementation and its tests before deciding whether it
   can be reused.
4. Remember that every file under `internal/sharedsteps` belongs to the same Go
   package. Unexported helpers, constants, and types can be reused across files.
   File boundaries do not make them scenario-private.

Useful discovery commands include:

```bash
rg -n -i '<concepts and synonyms>' e2e/acceptance
rg -n -F '<significant literal>' e2e/acceptance
rg -n '^(func|type|const|var) |^\s+[A-Za-z_][A-Za-z0-9_]*\s*=' \
  e2e/acceptance/internal/sharedsteps e2e/acceptance/framework \
  --glob '*.go'
```

Reuse an existing implementation when its semantics match and doing so reduces
code or makes the implementation easier to understand and maintain. Routine,
obvious reuse is an implementation choice and does not require separate user
approval or inclusion in the design proposal.

Do not force reuse merely because implementations look similar or constants
have the same value. Keep code local when sharing it would introduce awkward
parameters, conditionals, indirection, surprising behavior, or less readable
call sites. Prefer sharing domain knowledge that could otherwise drift, such as
resource names, selectors, paths, parsing rules, and discovery behavior.

When reuse would require a non-trivial refactor or broadening an existing
abstraction, or when its semantic equivalence is uncertain, explain the
tradeoff in the scenario design. Ask the user when the preferred direction is
not clear.

Gather enough information to answer:

- What user-visible behavior is being introduced or changed?
- What product capability or user or operator workflow does the scenario
  protect, and why does it belong in E2E rather than a lower-level test?
- Which Soperator versions have the old and new behavior?
- What cluster configuration and capacity does the scenario require, and for
  each unmet prerequisite should the scenario skip or fail?
- Which action should the scenario perform, and what observations prove that it
  worked?
- Is the scenario long (typically four minutes or more), and should it stay in
  default manual runs or use the heavy tag?
- What cluster state will the scenario mutate? For each persistent mutation,
  could the resulting state affect later scenarios or a rerun in any way, harm
  cluster health, consume resources, or incur cost? Which mutations therefore
  require cleanup?
- What timing, stability, and diagnostic requirements are known?

If material requirements are missing or conflicting, ask the user instead of
inventing them.

Before editing code, propose a scenario design for user approval. Include:

- the intended `.feature` changes, preferably with draft Gherkin;
- the version, heavy, unstable, and descriptive tag decisions;
- the skip-versus-fail decisions and prerequisite checks;
- any non-obvious reuse or refactoring decision that materially affects the
  implementation, including alternatives when the best choice is uncertain;
- the important implementation choices, helpers, polling, cleanup, and tests;
- the focused command and cluster shape needed for live validation.

Do not implement a new scenario until the user approves this design. If later
investigation materially changes it, explain the change and obtain approval
again.

## Scenario design

- Describe a user- or operator-visible product workflow in Gherkin. For a bug
  fix, test the product behavior that the bug broke instead of encoding an
  implementation-specific regression or isolated corner case. If there is no
  meaningful product workflow that would fail, prefer a unit, controller,
  Helm, or integration test. Keep kubectl, Slurm command syntax, parsing,
  retries, and other implementation details in Go steps.
- Keep scenarios independent. Do not rely on scenario execution order or state
  left by another scenario. Independence does not require reversing every
  successful product operation: use the cleanup criteria below to decide
  whether its resulting state may safely remain.
- Reuse an existing step only when its wording and semantics mean exactly the
  same thing. Prefer a new precise step to broadening an existing one until its
  behavior becomes surprising.
- Decide whether a scenario is heavy during design, before implementation.
  `@heavy` usually marks long scenarios (about four minutes or more), but this
  is a guideline: very important coverage may stay untagged in default manual
  runs. Heavy scenarios are excluded from manual CLI, GitHub UI, and PR runs
  by default, included by `--run-heavy`, workflow input `run_heavy=true`, or
  `/e2e-full`, and always included in periodic runs. If a long scenario's
  importance is unclear, explicitly ask the user whether to add `@heavy` and
  explain its default manual exclusion, manual opt-in, and periodic inclusion.
  Classify heavy independently of unstable and version constraints. Update
  the expectations in `acceptance/features_test.go` when changing membership.
- Decide skip versus failure explicitly for every prerequisite. Return
  `godog.ErrSkip` only when a valid target cluster does not provide an optional
  prerequisite, such as GPU capacity or a particular topology. Fail when the
  prerequisite is configured but unhealthy or insufficient, when the scenario
  cannot determine cluster state, or when product behavior is wrong. Never use
  a skip to hide flakiness, a broken environment that should satisfy the test,
  or an unfinished implementation.

## Tags and versioned behavior

The shared feature suite has four tag forms that affect execution:

- exactly one scenario-level `@soperator_version_...` constraint;
- optional `@heavy`;
- optional `@unstable`.
- optional `@sequential`, for scenarios that require exclusive execution. The
  standalone runner executes these first, one at a time, then runs all unmarked
  scenarios concurrently (four by default). Do not use `@parallel`.

By default, queue `@heavy` scenarios first in the parallel suite so long checks can
overlap shorter ones. `ConcurrentSuites` enforces this automatically while keeping
the original order within each group and leaving the sequential suite unchanged.
Worker reservations still control resource availability; launch order must never
be a scenario prerequisite.
Explicit `--heavy-first=false` and `--random-seed` overrides are available for
benchmarking and independence checks. Random ordering applies only to the
parallel suite and records a seed; do not change scenario safety tags or worker
reservations to support an ordering experiment.

Design every scenario to pass on its own and in any order. Create or verify its
prerequisites instead of depending on accounts, files or configuration left by
another scenario. Track jobs by ID and use scenario-specific paths, SSH identities
and artifact directories. Scope assertions and cleanup to owned resources: other
scenarios may legitimately drain or replace unreserved workers, and cancelling jobs
by a shared user or name can stop their work. Synchronize unavoidable shared setup,
such as first-time user creation, across the complete read-modify-write operation.

Before recording baselines or starting work, identify every worker the scenario
affects, including workers used by indirectly launched jobs. Acquire the complete
set atomically through `WorkerSelector.Acquire` or its `Pick...Workers` wrappers;
custom selectors must call `Reserve` before using their selection. An idle worker
found through a snapshot is not necessarily unreserved. Let acquisition wait for
busy or unhealthy configured workers rather than skipping them or holding a subset
while waiting for more. Pin every job to the reserved workers, use
`RequireReservedHost` before host-wide faults, and retain reservations until owned
jobs stop and required cleanup and recovery are verified. Report unsafe cleanup
failures through `reportCleanupErrorf` so the finalizer can quarantine the workers.
Worker reservations do not protect shared login Pods, cluster configuration or
shared files; isolate or narrowly synchronize those resources, or use the
documented `@sequential` exclusion below.

Use `@sequential` when a concrete conflict remains: shared configuration or jail
package mutation, shared login/system restarts, cluster-wide health assertions,
or custom worker selection outside the reservation pool. A shared read, multiple
workers, GPU usage, or a long runtime alone is not a reason to serialize a scenario.
Never add the tag just to hide a failure or incomplete concurrency review.

Every `@sequential` tag must be scenario-level and have a `# Sequential: ...`
comment immediately above its tag line. Name the conflicting resource or assertion
and explain why worker reservations do not currently isolate it. For example:

```gherkin
  # Sequential: Changes shared Slurm configuration used by every worker job.
  @soperator_version_>=5.0.0 @sequential
  Scenario: A custom Slurm setting is applied and restored
```

Keep the comment accurate when implementation changes; remove the tag and comment
when the conflict is resolved. Outline examples share the scenario's decision.
Run the feature-policy and concurrent-suite tests after changing membership.

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

Treat filenames as ownership and discovery signals. When a helper, constant,
or type is used by unrelated step families, move it from a scenario-named file
into a neutral domain file such as `job_helpers.go`, `ssh_helpers.go`, or
`nodeset_helpers.go`. Declarations shared only within one coherent component or
scenario family may remain with that family. All files in
`internal/sharedsteps` belong to one Go package; a declaration is not private
to the file where it is defined.

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

Clean up external state when leaving it behind could:

- affect later scenarios in any way, including their setup, behavior, outcome,
  duration, or reliability;
- affect a rerun of the same scenario;
- leave the cluster degraded, for example with a broken, drained, cordoned, or
  unready node;
- leave a disruptive process, job, or temporary resource running;
- consume scarce or billable resources; or
- leave shared configuration in a state that is not a healthy, supported
  baseline for the rest of the suite.

When cleanup is required, record the original value before the mutation so it
can also be restored after partial execution.

Do not restore state merely because the scenario changed it. A successful
product operation may leave the cluster in a new healthy and supported steady
state when the scenario can run first, later scenarios and reruns are unaffected
by the resulting state, and the remaining state has no operational or cost
impact. For example, a node-group update that intentionally restarts or
recreates workers does not need to be reversed if the resulting workers are
healthy and compatible with the rest of the suite. Reversing it would only
repeat an expensive disruption. In contrast, a node deliberately broken,
drained, or cordoned for a test must be repaired and returned to service.

Keep cleanup cheap and idempotent. Do not add complicated cleanup for harmless
state that satisfies the criteria above. When cleanup is best-effort, log
failures with enough resource identifiers to handle them manually; cleanup
must not replace the scenario's original failure.

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
