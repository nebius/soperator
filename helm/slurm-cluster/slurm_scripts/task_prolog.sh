#!/bin/bash

set -euxo pipefail

task_prolog="/mnt/jail$CHECKS_OUTPUTS_BASE_DIR/task_prolog"
(umask 000; mkdir -p "$task_prolog")
(
    printf "SLURM_CPU_BIND=%q\n" "${SLURM_CPU_BIND:-}"
    printf "SOPERATOR_CHECKS_RUNNER_CPU_BIND=%q\n" "${SOPERATOR_CHECKS_RUNNER_CPU_BIND:-}"
) >> "$task_prolog/$SLURMD_NODENAME.$SLURM_JOB_ID.$SLURM_STEP_ID.out"

if [ -v SOPERATOR_CHECKS_RUNNER_CPU_BIND ] && [ "${SOPERATOR_CHECKS_RUNNER_CPU_BIND:-}" = "${SLURM_CPU_BIND:-}" ]; then
    echo print "A recursive srun was detected. Unset SOPERATOR_CHECKS_RUNNER_CPU_BIND, if you think this is fine.">&2
fi

if [ -v SLURM_CPU_BIND ]; then
    echo export "SOPERATOR_CHECKS_RUNNER_CPU_BIND=${SLURM_CPU_BIND:-}"
fi
