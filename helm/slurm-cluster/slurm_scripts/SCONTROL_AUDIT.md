# Passive check scontrol audit

Temporary dev-only instrumentation for SCHED-1897 can log every `scontrol`
invocation made by passive checks. It is disabled by default.

Enable on a dev cluster:

```bash
helm upgrade <release> helm/slurm-cluster \
  --namespace <namespace> \
  --reuse-values \
  --set slurmScripts.scontrolAudit.enabled=true
```

Optional values:

```yaml
slurmScripts:
  scontrolAudit:
    enabled: true
    logPath: ""
    realScontrolPath: /usr/bin/scontrol
```

When `logPath` is empty, the wrapper writes to
`/opt/soperator-outputs/slurm_scripts/scontrol_audit.jsonl` inside the jail
and `/mnt/jail/opt/soperator-outputs/slurm_scripts/scontrol_audit.jsonl`
outside the jail. Each line is one JSON object.

Disable by setting `slurmScripts.scontrolAudit.enabled=false` and rolling the
updated scripts out again.

## Useful fields

- `context`: `prolog`, `epilog`, or `hc_program`
- `check_name`: current check name, or empty for runner-level filtering
- `check_phase`: usually `run_check` or `filter_by_node_state`
- `runner_invocation_id`: one `check_runner.py` invocation; useful for per-job
  or per-HealthCheckProgram-interval grouping
- `node`, `job_id`, `job_gpus`
- `command_class`: `show node`, `show job`, `listjobs`, `update drain`,
  `update resume`, `update comment`, or another first-level command
- `argv`: full `scontrol` argument array
- `caller_exe`, `caller_cmdline`: parent-process clue

## Collection examples

From a worker pod:

```bash
kubectl -n <namespace> exec <worker-pod> -c slurmd -- \
  cat /mnt/jail/opt/soperator-outputs/slurm_scripts/scontrol_audit.jsonl \
  > scontrol_audit.<worker-pod>.jsonl
```

From inside a node container:

```bash
cat /mnt/jail/opt/soperator-outputs/slurm_scripts/scontrol_audit.jsonl
cat /opt/soperator-outputs/slurm_scripts/scontrol_audit.jsonl
```

Combine several workers:

```bash
cat scontrol_audit.*.jsonl > scontrol_audit.all.jsonl
```

## Summary commands

Counts by context, check, and command class:

```bash
jq -r '[.context, (.check_name // ""), .command_class] | @tsv' \
  scontrol_audit.all.jsonl | sort | uniq -c | sort -nr
```

Counts by job and node:

```bash
jq -r 'select(.job_id != "") |
  [.job_id, .node, .context, (.check_name // ""), .command_class] | @tsv' \
  scontrol_audit.all.jsonl | sort | uniq -c | sort -nr
```

Counts per `HealthCheckProgram` runner invocation:

```bash
jq -r 'select(.context == "hc_program") |
  [.runner_invocation_id, .node, (.check_name // ""), .command_class] | @tsv' \
  scontrol_audit.all.jsonl | sort | uniq -c | sort -nr
```

Background versus job-coupled load:

```bash
jq -r '[
  (if .job_id == "" then "background" else "job" end),
  .context,
  (.check_name // ""),
  .command_class
] | @tsv' scontrol_audit.all.jsonl | sort | uniq -c | sort -nr
```

Sample table shape:

```text
count  context     check_name                 command_class
1240   hc_program  job_tmpfs_delete_leftover  listjobs
312    prolog      alloc_mem_used             show job
156    hc_program  alloc_mem_used             show node
18     prolog      gpu_health_check           update drain
9      hc_program  alloc_gpus_busy            update resume
```

## Static expectations

Based on the built-in check configs and scripts:

- `check_runner.py` may call `scontrol show node ... --json` for node-state
  filtering, `CHECKS_NODE_*` env export, and drain/undrain/comment decisions.
  The result is cached within one runner invocation until an update clears it.
- `alloc_mem_used` in `prolog` requests `CHECKS_JOB_ALLOC_MEM_BYTES`, so it is
  expected to cause one cached `scontrol show job ... --json` per runner
  invocation where the check runs.
- `alloc_mem_used` in `hc_program` requests `CHECKS_NODE_REAL_MEM_BYTES`, so it
  is expected to use `scontrol show node ... --json`.
- `alloc_gpus_busy` and `alloc_mem_used` undrain checks run only on drained
  nodes, so `hc_program` node-state filtering is expected to use
  `scontrol show node ... --json` when those checks are enabled.
- Failed checks with `on_fail: drain` can call `scontrol update ... State=drain`:
  `alloc_gpus_busy`, `alloc_mem_used`, `boot_disk_full`, `gpu_health_check`,
  and `nvme_raid_health` when that optional check is enabled.
- Successful checks with `on_ok: undrain` can call
  `scontrol update ... State=resume`: `alloc_gpus_busy`, `alloc_mem_used`, and
  `boot_disk_full` when the node reason matches the check reason.
- `job_tmpfs_delete_leftover` in `hc_program` directly calls
  `scontrol listjobs --json`, but only after it finds existing job tmpfs
  directories to evaluate.
- Other built-in passive checks do not contain executable `scontrol` calls;
  references in their output text are remediation hints only.
