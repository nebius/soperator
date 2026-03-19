#!/bin/bash

set -euxo pipefail

# Runs once at the job beginning without `SLURM_STEP_ID` and for each step with `SLURM_STEP_ID`.

if [ "${SLURM_LOCALID}" = "0" ]; then
    export CHECKS_OUTPUTS_BASE_DIR="/opt/soperator-outputs"
    task_prolog="$CHECKS_OUTPUTS_BASE_DIR/task_prolog"
    (umask 000; mkdir -p "$task_prolog")
    (
        printf "Slurm environment variables for task prolog:\n"
        env | grep -E '^(SLURM_|SLURMD_|SRUN_|SBATCH_)' || true
        printf "\n"
    ) > "$task_prolog/$SLURMD_NODENAME.$SLURM_JOB_ID.${SLURM_STEP_ID:-}.${SLURM_ARRAY_TASK_ID:-}.out"
fi

if [ -v SLURM_JOB_ID_SOPERATOR_TASK_PROLOG ] && [ "${SLURM_JOB_ID_SOPERATOR_TASK_PROLOG:-}" != "${SLURM_JOB_ID:-}" ] && [ -z "${SOPERATOR_SURPRESS_RECURSIVE_SRUN:-}" ]; then
    echo print "A Job from another Job was detected. Set SOPERATOR_SURPRESS_RECURSIVE_SRUN, if you think this is fine.">&2
fi

if [ -v SLURM_JOB_ID ]; then
    echo export "SLURM_JOB_ID_SOPERATOR_TASK_PROLOG=${SLURM_JOB_ID:-}"
fi
