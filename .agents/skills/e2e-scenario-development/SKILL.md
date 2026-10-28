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

Review concurrency explicitly: standalone scenarios run concurrently by default.
Reserve every required worker before use and keep reservations through cleanup;
isolate shared state or synchronize its setup. Use `@sequential` only for a
concrete conflict that reservations do not resolve. Put a specific
`# Sequential: ...` reason immediately above every scenario-level `@sequential`
tag line. Follow `e2e/AGENTS.md` for the decision criteria and validation; do not
use `@parallel`. Include the concurrency decision in the scenario design, and
update the reason or remove the exclusion when its underlying conflict changes.

Decide `@heavy` membership during scenario design. Heavy scenarios are excluded
from manual runs by default and are usually long (about four minutes or more),
but very important coverage may remain untagged. If a long scenario's importance
is unclear, explicitly ask whether to add `@heavy`, explaining that it excludes
the scenario from manual runs by default while periodic runs still include it.
Follow the tag policy in `e2e/AGENTS.md` for opt-in controls and inventory updates.
