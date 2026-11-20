---
name: e2e-scenario-development
description: Plan and implement new or changed Soperator E2E acceptance scenarios. Use when a user asks to design, add, or modify a scenario under e2e/acceptance, including gathering requirements, proposing Gherkin and implementation details, and validating the result.
---

# E2E scenario development

Read `e2e/AGENTS.md` completely and follow its scenario-development workflow.
Treat the mandatory reuse audit as blocking and complete it before proposing a
design.

Gather any missing product and test requirements, then propose the scenario
design before editing. Do not implement until the user approves that design. If
later investigation materially changes the approved design, explain the change
and obtain approval again.

Use `e2e/README.md` for runner usage.

Include the concurrency decision in the scenario design. The standalone runner
currently executes sequential exclusions followed by the remaining scenarios,
both at concurrency 1; native reservation enforcement is opt-in during migration.
Design independent state and acquire the complete worker set before baselines
or mutations. Bind all direct and indirect Slurm allocations through the shared
reservation-aware helpers, including user switches. Retain ownership through
producer shutdown, cleanup and recovery, and report required restoration
failures with `RecordWorkerCleanupError`, including on skipped scenarios.

Use `@sequential` for a concrete conflict that reservations do not isolate.
Put a specific `# Sequential: ...` reason immediately above every scenario-level
tag line. Follow `e2e/AGENTS.md` for the criteria and feature-policy tests; do not
use `@parallel`. Remove the exclusion and reason when the conflict is resolved.

Decide `@heavy` membership during scenario design. Heavy scenarios are excluded
from manual runs by default and are usually long (about four minutes or more),
but very important coverage may remain untagged. If a long scenario's importance
is unclear, explicitly ask whether to add `@heavy`, explaining that it excludes
the scenario from manual runs by default while periodic runs still include it.
Follow the tag policy in `e2e/AGENTS.md` for opt-in controls.
