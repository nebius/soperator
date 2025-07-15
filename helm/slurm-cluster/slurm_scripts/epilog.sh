#!/bin/bash

set -eox pipefail

export JAIL_DIR="/mnt/jail"
export LOGS_OUTPUT_DIR="/opt/soperator-outputs/slurm_scripts"
export SCRIPT_CONTEXT="epilog"

(umask 000; mkdir -p ${JAIL_DIR}${LOGS_OUTPUT_DIR})

if [ -n "$SLURM_JOB_GPUS" ]; then
    echo "Execute healthchecks in jail after GPU jobs"
    chroot /mnt/jail /bin/bash -s <<-'EOF'
        set -eox pipefail

        # The list of healthchecks in the execution order
        checks=(
            health_checker
        )

        GPU_COUNT=$(nvidia-smi --list-gpus 2>/dev/null | wc -l || echo 0)
        echo "Found ${GPU_COUNT} GPUs"

        # Only add hc_* checks if we have exactly 8 GPUs
        if [[ "${GPU_COUNT}" -eq 8 ]]; then
            checks+=(
                hc_xid
                hc_ib_link_state
                hc_ib_counters
                hc_ib_pkey
            )
        else
            echo "Skipping hc_* checks because GPU_COUNT=${GPU_COUNT} (need 8)"
        fi

        pushd /opt/slurm_scripts || exit 0
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

                # Drain the Slurm node if it's not yet drained by the check script
                cur_state=$(sinfo -n "${SLURMD_NODENAME}" --Format=StateLong --noheader || true)
                if [[ -n "${cur_state}" && "${cur_state}" != "draining" && "${cur_state}" != "drained" ]]; then
                    reason="[HC] Failed ${check} [${SCRIPT_CONTEXT}]"
                    if [[ -n "${details}" ]]; then
                        reason="[HC] Failed ${check}: ${details} [${SCRIPT_CONTEXT}]"
                    fi
                    echo "Drain Slurm node ${SLURMD_NODENAME}"
                    scontrol update NodeName="${SLURMD_NODENAME}" State=drain Reason="${reason}" || true
                fi

                exit 0
            fi
        done
        popd || exit 0
EOF
fi

echo "Unmap the Slurm job with DCGM metrics"
log="${JAIL_DIR}${LOGS_OUTPUT_DIR}/${SLURMD_NODENAME}.unmap_job_dcgm.${SCRIPT_CONTEXT}.out"
bash /mnt/jail/opt/slurm_scripts/unmap_job_dcgm.sh > "$log" 2>&1 || true

exit 0
