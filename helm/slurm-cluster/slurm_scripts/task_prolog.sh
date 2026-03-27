#!/bin/bash

set -euxo pipefail

# Runs once at the job beginning without `SLURM_STEP_ID` and for each step with `SLURM_STEP_ID`.

if [ "${SLURM_LOCALID}" = "0" ]; then
    cgroup=$(cat /proc/self/cgroup)
    cgroup="${cgroup#0::}"
    # drop basename until it is user
    while [ -n "$cgroup" ] && [ "${cgroup##*/}" != "user" ]; do
    cgroup="${cgroup%/*}"
    done
    if [ -n "$cgroup" ]; then
        echo export "DOCKER_HOST=unix:///var/run/soperator-docker.sock"
        echo export "DOCKER_CUSTOM_HEADERS=Cgroup-Parent=$cgroup"
    fi

    export CHECKS_OUTPUTS_BASE_DIR="/opt/soperator-outputs"
    task_prolog="$CHECKS_OUTPUTS_BASE_DIR/task_prolog"
    (umask 000; mkdir -p "$task_prolog")
    (
        printf "Slurm environment variables for task prolog:\n"
        env | grep -E '^(SLURM_|SLURMD_|SRUN_|SBATCH_)' || true
        printf "\n"
    ) > "$task_prolog/$SLURMD_NODENAME.$SLURM_JOB_ID.${SLURM_STEP_ID:-}.${SLURM_ARRAY_TASK_ID:-}.out"
fi

if [ -v SLURM_JOB_ID_SOPERATOR_TASK_PROLOG ] && [ "${SLURM_JOB_ID_SOPERATOR_TASK_PROLOG:-}" != "${SLURM_JOB_ID:-}" ] && [ -z "${SOPERATOR_SUPPRESS_RECURSIVE_SRUN:-}" ]; then
    echo print "A job submission from within another job was detected. If this behavior is intentional, set SOPERATOR_SUPPRESS_RECURSIVE_SRUN=1."
fi

if [ -v SLURM_JOB_ID ]; then
    echo export "SLURM_JOB_ID_SOPERATOR_TASK_PROLOG=${SLURM_JOB_ID:-}"
fi

if ! /usr/bin/python3 -c "import sys; sys.exit(0)" >/dev/null 2>&1; then
    echo "Python is not installed or not working" >&2
    exit 0
fi

if [ -z "${SLURM_JOB_ID:-}" ]; then
    exit 0
fi
