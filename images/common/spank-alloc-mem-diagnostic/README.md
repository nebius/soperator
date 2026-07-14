# Allocated-memory SPANK diagnostic

This plugin records the values exposed to `slurm_spank_init()` by a remote
`slurmstepd`. It is diagnostic only: it does not change the job environment,
write files, drain nodes, or return an error. The only intentional delay is the
optional `sleep=<seconds>` argument.

The plugin ignores every context except `S_CTX_REMOTE`. In remote context it
logs one `event=init` record containing:

- an explicit UTC timestamp and PID;
- the SPANK context;
- job, step, and relative node IDs;
- job- and step-allocated memory in MB;
- the numeric and textual return code from every `spank_get_item()` call; and
- the configured sleep value.

An unavailable item is logged as `unavailable`, alongside its non-success
return code. If sleep is enabled, a second `event=sleep_complete` record is
written after the delay.

## Build and enable

The worker, login, and Slurm check-job images build
`spank_alloc_mem_diagnostic.so` using the same GNU99, PIC, LTO, optimization,
Slurm include/library paths, and shared-library layout as the existing
Soperator NCCL debug SPANK plugin. Rebuild and deploy those images before
enabling the plugin.

Append the plugin to the SlurmCluster's custom SPANK plugins:

```yaml
spec:
  plugStackConfig:
    customPlugins:
      - required: false
        path: spank_alloc_mem_diagnostic.so
```

Keep it `optional` so a packaging or loading problem cannot block a job. After
Soperator regenerates `plugstack.conf`, confirm that it contains:

```text
optional spank_alloc_mem_diagnostic.so
```

The shared library is installed under
`/usr/lib/<architecture>-linux-gnu/slurm/`, which is already a Slurm plugin
search location. It is also made available inside the Soperator jail so an
`srun` launched from a job can load the same plugstack.

To watch diagnostic records on a development cluster, use the worker pod logs
(adjust the pod name if needed):

```bash
kubectl -n soperator logs worker-0 --all-containers=true -f |
  grep --line-buffered alloc_mem_diagnostic
```

The important fields look like:

```text
event=init ... context=remote ... job_id=123 job_id_rc=0(Success) step_id=4294967294 step_id_rc=0(Success) node_id=0 node_id_rc=0(Success) job_alloc_mem_mb=4096 job_alloc_mem_rc=0(Success) step_alloc_mem_mb=4096 step_alloc_mem_rc=0(Success)
```

Only values whose return code is `ESPANK_SUCCESS` are usable. Slurm reports
`S_JOB_ALLOC_MEM` and `S_STEP_ALLOC_MEM` in MB. Slurm represents special
steps such as the batch script with reserved unsigned 32-bit values, so a batch
`step_id` can appear as a large integer.

## Experiment 1: allocated job memory

With the plugin enabled without a sleep argument, submit:

```bash
sbatch --mem=4G --wrap='hostname; sleep 30'
```

Find the `event=init` record for the returned job ID. Verify:

1. `context=remote` proves the plugin ran in `slurmstepd`.
2. `job_alloc_mem_rc=0(Success)` proves `S_JOB_ALLOC_MEM` was available.
3. `job_alloc_mem_mb=4096` matches `--mem=4G`.
4. The other item return codes and values show what else is available at this
   point in job launch.

For a multi-node allocation, expect a record from every participating
`slurmstepd`; compare `node_id` across worker logs.

## Experiment 2: callback timing

Temporarily configure the custom plugin argument:

```yaml
spec:
  plugStackConfig:
    customPlugins:
      - required: false
        path: spank_alloc_mem_diagnostic.so
        arguments:
          sleep: "10"
```

This renders the plugstack entry:

```text
optional spank_alloc_mem_diagnostic.so sleep=10
```

Then submit:

```bash
sbatch --mem=4G --wrap='date; hostname'
```

Compare the plugin's `event=init` and `event=sleep_complete` timestamps with
the timestamp in the job output. The two plugin timestamps should be about ten
seconds apart, and the payload should start roughly ten seconds after
`event=init`, immediately after `event=sleep_complete`. Comparing these
timestamps avoids confusing scheduler queue time with the plugin delay.

The argument applies to every remote step that loads the plugin. Remove it
before running the other experiments unless repeated ten-second delays are
intentional.

## Experiment 3: multiple `srun` steps

Submit a one-node script so one log line corresponds to one remote
`slurmstepd` invocation:

```bash
cat > /tmp/spank-steps.sbatch <<'EOF'
#!/bin/bash
#SBATCH --nodes=1
#SBATCH --mem=4G

echo "batch job: ${SLURM_JOB_ID}"
srun --nodes=1 --ntasks=1 sh -c 'echo first step: ${SLURM_STEP_ID}; hostname'
srun --nodes=1 --ntasks=1 sh -c 'echo second step: ${SLURM_STEP_ID}; hostname'
EOF

sbatch /tmp/spank-steps.sbatch
```

Filter worker logs by the returned job ID. Count distinct PIDs and
`step_id` values. Record whether there is one callback for the batch step plus
one for each `srun` step, and compare the numeric IDs with
`SLURM_STEP_ID` printed by the two payloads. On a multi-node step, group by
`step_id` and `node_id`: remote initialization is performed by each
participating node's `slurmstepd`.

## Experiment 4: interactive allocation

Start an allocation from a login node and launch two steps:

```bash
salloc --nodes=1 --mem=4G
srun --nodes=1 --ntasks=1 sh -c 'echo interactive step: ${SLURM_STEP_ID}; hostname'
srun --nodes=1 --ntasks=1 sh -c 'echo second interactive step: ${SLURM_STEP_ID}; hostname'
exit
```

Compare these worker records with Experiment 3. In particular, note whether
creating the allocation itself produces a remote record and which records are
produced by each `srun`. The plugin intentionally ignores the allocator and
local contexts, so every diagnostic record should still say
`context=remote`.

When testing is complete, remove the custom plugin entry from
`spec.plugStackConfig.customPlugins`.
