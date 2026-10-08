# Passive checks

Passive checks are scripts that Slurm runs on every worker node by itself: in the job prolog, in the job
epilog and periodically from `HealthCheckProgram`. They do not submit jobs or create Kubernetes resources,
which is what distinguishes them from [active checks](active-checks.md). A failed passive check drains or
comments the Slurm node, and a later successful run can undrain it.

## How a check is run

The `slurm-cluster` chart ships the scripts in `helm/slurm-cluster/slurm_scripts/` as a ConfigMap mounted
at `/opt/slurm_scripts/` in the worker container and in the jail. Slurm calls
`prolog.sh`, `epilog.sh` or `hc_program.sh` from there; each sets `CHECKS_CONTEXT` and starts `check_runner.py`.

The runner:

1. Reads the node metadata written at pod start to `/run/soperator/node_metadata.env` and exports it to
   every check: `NODESET_GPU_ENABLED`, `CHECKS_PLATFORM_TAG`, `CHECKS_PLATFORM_TAGS`, `CHECKS_NODE_REAL_MEM_BYTES`.
2. Loads `checks.json`, which the chart assembles from the `<script>.json` file next to every enabled
   built-in script plus the `extra` entries in values.
3. Filters the checks by context, platform, node state and job shape (see the config fields below).
4. Runs the host checks from `/opt/slurm_scripts/`, then does `chroot /mnt/jail` and runs the jail checks.
5. Stops at the first failed check. In the prolog context it also exits with 1, so Slurm requeues the job
   and prints `Slurm healthcheck failed on node <name>` to the job output.

That exit 1 is the only one the runner allows itself. A missing config or metadata file is logged and the
runner exits 0, because any other non-zero exit from the prolog makes Slurm itself drain the node with the
reason `Prolog error`. That reason carries none of the prefixes below, so no controller replaces the node
and no check undrains it; it stays drained until somebody resumes it by hand.

`hc_program` runs every 120 seconds on nodes in any state (`HealthCheckInterval` and
`HealthCheckNodeState=ANY,CYCLE` in the SlurmCluster CR template, overridden with `healthCheckConfig` in the
chart values). Running on drained nodes is what lets a check undrain them.

A user can skip the prolog and epilog checks for one job with `--comment skip_checks`, for example to debug
a node that a check keeps draining; hc_program is unaffected. TaskProlog is not part of this framework: it
runs inside the job's container rootfs, where `/opt/slurm_scripts` does not exist.

Runner output goes to `/opt/soperator-outputs/local/slurm_scripts/<node>.check_runner.<context>.out` in the
jail. Each check gets its own log file, see the `log` field below.

## Check contract

A check is any executable that the runner starts as `bash -l -c "<command> 3>&1 1><log> 2>&1"`:

- Exit code 0 means OK, any other exit code means FAIL. There is no third state. If the check cannot decide,
  for example because a tool it needs is missing, it must log the problem and exit 0; otherwise an unrelated
  breakage drains the node.
- stdout and stderr go to the check's log file, so the check can be as verbose as it likes there.
- File descriptor 3 is the details channel. Whatever the check writes to it is appended to the drain or
  comment reason (`echo "..." >&3` in bash, `os.write(3, ...)` in Python). Writing to it outside the runner
  fails with `EBADF`, so run checks by hand through the runner or redirect fd 3 explicitly.
- The runner escapes the details with `unicode_escape` and does not truncate them.

Time budget. The prolog runs before every job on every node it lands on, and Slurm gives a batch job
`BatchStartTimeout`, 10 seconds by default and not changed by soperator, to launch including the whole
prolog; past that the job is considered missing and the allocation is released. The runner, its filtering and
every prolog check share those 10 seconds, so a prolog check has to finish in well under a second. Slurm
kills `HealthCheckProgram` after 60 seconds, which caps all hc_program checks together. The epilog has no
effective limit, but the node stays in `COMPLETING` until it ends. Anything slow, for example a DCGM
diagnostic that takes 15 seconds, belongs in epilog or hc_program only.

Controller RPCs. `scontrol show node`, `scontrol show job` and `scontrol update` are requests to slurmctld,
multiplied by the number of jobs when issued from prolog or epilog. An undrain needs two of them, one to
read the current drain reason and one to resume the node, so `on_ok` is honoured only in hc_program, where
it costs one query per node every 120 seconds instead of one per job. The `need_env` variables are opt-in
for the same reason. The node metadata in the environment, `scontrol show hostnames` and
`scontrol listjobs` are answered locally.

Language. The images ship `/usr/bin/python3` 3.12 with the standard library only. Bash is fine for a few
lines; anything that parses output goes to Python and reads `--json` where Slurm offers it.

## Reasons

The reason is built as `<reason_base>[: <details>] [<context>]`, for example
`[software_problem] nvidia_libraries: libcuda.so.1: file too short [hc_program]`. It is set with
`scontrol update State=DRAIN Reason=...` or written to the node comment, depending on `on_fail`.

The prefix decides what the automation does with the node:

| Prefix | Who reacts | Effect |
|---|---|---|
| `[node_problem]` | soperatorchecks, updatecontroller | Node is marked unhealthy and replaced when node replacement is enabled. The worker rollout waits for the replacement. |
| `[hardware_problem]` | soperatorchecks, updatecontroller | Same as `[node_problem]`. Nothing in soperator sets it; it is reserved for tooling outside the operator. |
| `[user_problem]` | nobody | Node stays drained. The worker rollout proceeds. Use it when the fix is on the user or support side, for example leftover processes or a full disk. |
| `[software_problem]` | nobody | Node stays drained. The worker rollout proceeds. Use it when the node itself is fine but something on it is broken and a pod restart or a support action fixes it, for example empty driver libraries in the jail. |

Matching is by substring, so the details must not contain another prefix or `Kill task failed`.

Keep the details short and predictable. The whole reason ends up as the `reason` label of the
`slurm_node_info` and `slurm_node_fails_total` metrics of the Slurm exporter, so every distinct value is a
new time series, and `sinfo -R` shows only its first 20 characters. Put the varying part in the log, not in
the reason:

- Name the component and the state, not the measurement: `libcuda.so.1: file too short` rather than
  `libcuda.so.1: 0 bytes`, `available memory below RealMemory` rather than the number of gigabytes.
- Do not pass through error text from other tools. It changes between versions, contains paths and may
  span several lines.
- Prefer a fixed vocabulary of a few phrases per check.
- Start the details with a lowercase letter, they follow `: `. Do not repeat the node name, Slurm shows it
  next to the reason anyway.

`on_ok: undrain` and `on_ok: uncomment` only work in the `hc_program` context and only when the current
reason starts with the check's `reason_base`, so a check never removes a drain set by somebody else. An
undrain script must confirm that the node has recovered, not merely avoid failing: a check that exits 0
because the node is busy must not be reused as an undrain condition. When a check is renamed or replaced,
its successor keeps the old `reason_base`, otherwise nodes drained before the upgrade stay drained forever.

## Config fields

Check names are nouns without the word `check`: `nvidia_libraries`, not `check_nvidia_libraries`. A check
that needs different behaviour in different contexts, for example drain in prolog and undrain in hc_program,
is split into `<name>.drain.<ext>` and `<name>.undrain.<ext>` sharing the same `name`, as `alloc_gpus_busy`
and `idle_mem_used` do. A check that ships disabled uses `contexts: ["none"]`.

Each `<script>.json` is one object with these fields:

| Field | Default | Meaning |
|---|---|---|
| `name` | `noname` | Used in logs, reasons and log file names. |
| `command` | `/usr/bin/true` | Run with `bash -l -c` from `/opt/slurm_scripts/`, so `./script.sh` and `/usr/bin/python3 ./script.py` work. |
| `platforms` | `["any"]` | `any`, `CPU`, `<N>xGPU` or `<N>x<MODEL>` where the model comes from the Gres type in upper case, for example `8xH100`. Use the `<N>xGPU` form for any GPU node. |
| `contexts` | `["any"]` | `any`, `none`, `prolog`, `epilog`, `hc_program`. |
| `node_states` | `["any"]` | `any` or `drain` to run only on drained nodes. |
| `skip_for_cpu_jobs` | `false` | Skip in prolog and epilog for jobs without GPUs. |
| `skip_for_partial_cpu_jobs` | `false` | Skip in prolog and epilog for CPU jobs that do not take the whole node. |
| `skip_for_partial_gpu_jobs` | `false` | Skip in prolog and epilog for jobs that do not take all GPUs. |
| `on_fail` | `none` | `none`, `drain` or `comment`. |
| `on_ok` | `none` | `none`, `undrain` or `uncomment`; hc_program only. |
| `reason_base` | `[node_problem] $name` | Prefix and check name; `$name` and `$context` are substituted. |
| `reason_append_details` | `true` | Append what the check wrote to fd 3. |
| `run_in_jail` | `false` | Run inside `chroot /mnt/jail`. Jail checks see what jobs see but only the tools installed in the jail image. |
| `log` | `slurm_scripts/$worker.$name.$context.out` | Log path relative to `/opt/soperator-outputs/local`. |
| `need_env` | `[]` | Extra variables that cost a controller RPC: `CHECKS_NODE_STATE_FLAGS`, `CHECKS_NODE_REASON`, `CHECKS_NODE_COMMENT`. |

## Adding a check

1. Put the script and its `<script>.json` into `helm/slurm-cluster/slurm_scripts/`. Both go through Helm
   `tpl`, as do `customConfig` and `extra` entries, so the script must not contain `{{`.
2. Add the script under `slurmScripts.builtIn` in `helm/slurm-cluster/values.yaml` with `enabled: true`.
   The key must equal the file name; the chart refuses to render a `builtIn` key without a bundled file.
3. Add a helm unittest in `helm/slurm-cluster/tests/` that asserts on the rendered `checks.json`, and a
   Python unit test next to the script for anything beyond a few lines of bash.
4. Run `make sync-version-from-scratch` and `helm unittest helm/slurm-cluster`.
5. Keep in mind that the acceptance tests fail on any `Check <name>: FAIL` in the runner output on a healthy
   cluster, so a check with false positives blocks every E2E run.

Users can disable a built-in check with `slurmScripts.builtIn.<script>.enabled: false`, replace its config
or content with `customContent` and `customConfig`, and add their own under `slurmScripts.extra`.
