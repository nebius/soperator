#!/bin/bash

set -eox pipefail

export JAIL_DIR="/mnt/jail"
export LOGS_OUTPUT_DIR="/opt/soperator-outputs/slurm_scripts"
export SCRIPT_CONTEXT="epilog"

mkdir -p ${JAIL_DIR}${LOGS_OUTPUT_DIR}
exec > ${JAIL_DIR}${LOGS_OUTPUT_DIR}/${SLURMD_NODENAME}.${SCRIPT_CONTEXT}.out 2>&1

if [ -n "$SLURM_JOB_GPUS" ]; then
    echo "Execute GPU healthchecks"
    chroot ${JAIL_DIR} /bin/bash -s <<-'EOF'
        set -eox pipefail

        # health-checker library uses os.Environ and doesn't see the whole PATH without explicit exporting
        export PATH=$PATH

        exit_code=0
        log="${LOGS_OUTPUT_DIR}/${SLURMD_NODENAME}.health-checker.${SCRIPT_CONTEXT}.out"
        details=$(health-checker run -e soperator -p {{ .Values.healthCheckScripts.platform }} -f mk8s-txt \
            -n {{ .Values.healthCheckScripts.epilog.checks }} 2>$log) || exit_code=$?
        printf "\n%s\n" $details >> $log

        if [[ $exit_code -eq 0 ]]; then
            echo "Check ${check}: PASS"
        elif [[ $exit_code -eq 1 ]]; then
            echo "health-checker: FAIL (${details})"

            # Drain the Slurm node if it's not yet drained by the hc_*.sh script
            cur_state=$(sinfo -n "${SLURMD_NODENAME}" --Format=StateLong --noheader || true)
            if [[ -n "${cur_state}" && "${cur_state}" != "draining" && "${cur_state}" != "drained" ]]; then
                reason="[HC] Failed health_checker [${SCRIPT_CONTEXT}]"

                echo "Drain Slurm node ${SLURMD_NODENAME}"
                scontrol update NodeName="${SLURMD_NODENAME}" State=drain Reason="${reason}" || true
            fi

            exit 0
        else
            echo "health-checker: Fail ${details}"
            exit 0
        fi
EOF
fi

echo "Unmap the Slurm job with DCGM metrics"
log="${JAIL_DIR}${LOGS_OUTPUT_DIR}/${SLURMD_NODENAME}.unmap_job_dcgm.${SCRIPT_CONTEXT}.out"
/mnt/jail/opt/soperator/custom-scripts/unmap_job_dcgm.sh > "$log" 2>&1 || true

exit 0
