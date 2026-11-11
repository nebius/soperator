# Slurm reservation prototype: later Bulbasaur validation

No cluster runs are part of preparing this draft. After approval:

1. Verify worker/Pod/host health, no leftover jobs or unsafe reservations, and
   manually suspend periodic GPU checks and drain their existing jobs. Preserve
   the original check settings for restoration after testing.
2. Pin one acceptance binary built from this branch. Use normal feature order
   and concurrency eight for one non-heavy run and one heavy-inclusive run.
   Keep the same operator version, worker shape, filters and other producers
   as the comparison runs. Use the existing two-hour worker acquisition limit.
3. Collect both suite reports, scenario timings, job outputs and acquisition/
   finalization logs. Check that every expected scenario ran once or has the
   same configuration-dependent skip; distinguish framework interference from
   known product flakes. Verify cluster health and no leaked leases/jobs after
   each run before proceeding.
4. Report sequential, parallel and total wall time, acquisition waits, and CPU/
   GPU lease occupancy separately. Occupancy includes disruption and recovery;
   it is not measured CPU/GPU hardware utilization.

The sequential inventory stays identical to the base branch. The only deliberate
coverage exclusion is **GPU ActiveCheck succeeds on all GPU workers**, newly
`@unstable` because its external submitter does not carry a reservation and cancels
jobs by shared name. It is also heavy, so this affects only the heavy comparison.
Do not count a previously skipped ActiveCheck as removed work.

Compare the exact scenario/outline-example intersection in the old and new
reports, including version/manual/unstable filters and skip status. Publish actual
suite wall times and the coverage difference alongside the comparison. Removing
one scenario's elapsed time from a concurrent suite's wall time is not a valid
adjustment: it may overlap other work or change the critical path. If the
ActiveCheck ran in the old benchmark, identify that limitation explicitly; use a
matching-filter rerun later if an exact wall-time comparison is needed.

Reservations are created with an eight-hour TTL and fixed explicit nodes, without
`REPLACE`, `REPLACE_DOWN`, `OVERLAP`, `IGNORE_JOBS`, `MAINT` or `FLEX`. Verify
on the cluster that replacement/restart retains exactly those logical workers,
another scenario can use the spare worker, whole-NodeSet requests acquire all
workers together, and CPU preference leaves GPU workers available.
