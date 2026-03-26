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

cgroup_parent=$(python3 <<PYTHON
with open("/proc/self/cgroup", "r", encoding="utf-8") as f:
    for line in f:
        entry = line.strip()
        if not entry:
            continue
        parts = entry.split(":", 2)
        if len(parts) != 3:
            continue
        cgroup_path = parts[2].strip()
        if cgroup_path:
            print(cgroup_path)
            break
PYTHON
)
echo export "DOCKER_HOST=unix:///var/run/docker-test.sock"
echo export "DOCKER_CUSTOM_HEADERS=Cgroup-Parent=$cgroup_parent"
