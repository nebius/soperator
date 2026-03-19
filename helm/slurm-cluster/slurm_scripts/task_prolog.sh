#!/bin/bash

set -euxo pipefail

export CHECKS_OUTPUTS_BASE_DIR="/opt/soperator-outputs"
task_prolog="$CHECKS_OUTPUTS_BASE_DIR/task_prolog"
(umask 000; mkdir -p "$task_prolog")
(
    printf "Slurm environment variables for task prolog:\n"
    env | grep -E '^(SLURM_|SLURMD_|SRUN_|SBATCH_)' || true
    printf "\n"
) > "$task_prolog/$SLURMD_NODENAME.$SLURM_JOB_ID.$SLURM_STEP_ID${SLURM_ARRAY_TASK_ID:+".$SLURM_ARRAY_TASK_ID"}.out"

if [ -v SOPERATOR_CHECKS_RUNNER_CPU_BIND ] && [ "${SOPERATOR_CHECKS_RUNNER_CPU_BIND:-}" = "${SLURM_CPU_BIND:-}" ]; then
    echo print "A recursive srun was detected. Unset SOPERATOR_CHECKS_RUNNER_CPU_BIND, if you think this is fine.">&2
fi

if [ -v SLURM_CPU_BIND ]; then
    echo export "SOPERATOR_CHECKS_RUNNER_CPU_BIND=${SLURM_CPU_BIND:-}"
fi
