#!/bin/bash

set -eox pipefail

export JAIL_DIR="/mnt/jail"
export LOGS_OUTPUT_DIR="/opt/soperator-outputs/slurm_scripts"
export SCRIPT_CONTEXT="prolog"

(umask 000; mkdir -p ${JAIL_DIR}${LOGS_OUTPUT_DIR})

if [ -n "$SLURM_JOB_GPUS" ]; then
    echo "Execute GPU healthchecks"
    chroot /mnt/jail /bin/bash -s <<-'EOF'
        set -eox pipefail

        # The list of healthchecks in the execution order
        checks=(
            health_checker
        )

        pushd /opt/slurm-scripts || exit 0
        for check in "${checks[@]}"; do
            script="${check}.sh"
            log="${LOGS_OUTPUT_DIR}/${SLURMD_NODENAME}.${check}.${SCRIPT_CONTEXT}.out"

            # Run the current script and:
            # - write its fd 1 and 2 (stderr+stdout) to the $log file
            # - store its fd 3 to $details
            echo "Run script ${script} (log to ${log})"
            if details=$(bash "${script}" 3>&1 1>"${log}" 2>&1); then
                echo "Check ${check}: PASS"

                # Continue to the next check
            else
                echo "Check ${check}: FAIL (${details})"

                # Drain the Slurm node if it's not yet drained by the hc_*.sh script
                cur_state=$(sinfo -n "${SLURMD_NODENAME}" --Format=StateLong --noheader || true)
                if [[ -n "${cur_state}" && "${cur_state}" != "draining" && "${cur_state}" != "drained" ]]; then
                    reason="[HC] Failed ${check} [${SCRIPT_CONTEXT}]"
                    if [[ -n "${details}" ]]; then
                        reason="[HC] Failed ${check}: ${details} [${SCRIPT_CONTEXT}]"
                    fi
                    echo "Drain Slurm node ${SLURMD_NODENAME}"
                    scontrol update NodeName="${SLURMD_NODENAME}" State=drain Reason="${reason}" || true
                fi

                # Notify the user by printing a message to the job output
                echo "print Slurm healthcheck failed on node ${SLURMD_NODENAME}, trying to automatically requeue"

                # Exit nonzero: prolog fails, job will be requeued
                exit 1
            fi
        done
        popd || exit 0
EOF
fi

echo "Cleanup leftover enroot containers"
log="${JAIL_DIR}${LOGS_OUTPUT_DIR}/${SLURMD_NODENAME}.cleanup_enroot.${SCRIPT_CONTEXT}.out"
bash /mnt/jail/opt/soperator/slurm-scripts/cleanup_enroot.sh  > "$log" 2>&1 || true

echo "Map the Slurm job with DCGM metrics"
log="${JAIL_DIR}${LOGS_OUTPUT_DIR}/${SLURMD_NODENAME}.map_job_dcgm.${SCRIPT_CONTEXT}.out"
bash /mnt/jail/opt/soperator/slurm-scripts/map_job_dcgm.sh > "$log" 2>&1 || true

exit 0
