#!/bin/bash

set -euxo pipefail

printf "SLURM_CPU_BIND=%q\n" "${SLURM_CPU_BIND:-}"
printf "SOPERATOR_CHECKS_RUNNER_CPU_BIND=%q\n" "${SOPERATOR_CHECKS_RUNNER_CPU_BIND:-}"

if [ -v SOPERATOR_CHECKS_RUNNER_CPU_BIND ] && [ "${SOPERATOR_CHECKS_RUNNER_CPU_BIND:-}" = "${SLURM_CPU_BIND:-}" ]; then
    echo "print CPU binding detected, unset SOPERATOR_CHECKS_RUNNER_CPU_BIND, if you think this is fine">&2
fi

if [ -v SLURM_CPU_BIND ]; then
    echo "export SOPERATOR_CHECKS_RUNNER_CPU_BIND=${SLURM_CPU_BIND}"
fi

if ! /usr/bin/python3 -c "import sys; sys.exit(0)" >/dev/null 2>&1; then
    echo "Python is not installed or not working" >&2
    exit 0
fi

export CHECKS_CONTEXT="task_prolog"
export CHECKS_CONFIG="/opt/slurm_scripts/checks.json"
export CHECKS_OUTPUTS_BASE_DIR="/opt/soperator-outputs"
export CHECKS_RUNNER_OUTPUT="/mnt/jail$CHECKS_OUTPUTS_BASE_DIR/slurm_scripts/$SLURMD_NODENAME.$SLURM_JOB_ID.check_runner.$CHECKS_CONTEXT.out"
export PATH="$PATH"

echo "Starting check_runner.py"
/usr/bin/python3 /opt/slurm_scripts/check_runner.py 2>&1
