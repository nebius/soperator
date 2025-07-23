#!/bin/bash

set -eox pipefail

export JAIL_DIR="/mnt/jail"
export LOGS_OUTPUT_DIR="/opt/soperator-outputs/slurm_scripts"
export SCRIPT_CONTEXT="hc_program"

(umask 000; mkdir -p ${JAIL_DIR}${LOGS_OUTPUT_DIR})

echo "Execute healthchecks in jail"
chroot /mnt/jail /bin/bash -s <<-'EOF'
    set -eox pipefail

    # The list of healthchecks in the execution order
    checks=(
        boot_disk_full
        health_checker
    )

    gpus_on_node=$(nvidia-smi --query-gpu=name --format=csv,noheader | sort | uniq -c)
    if [[ "${gpus_on_node}" == *"8 NVIDIA"* ]]; then
        checks+=(
            hc_host_service
            hc_xid
            hc_ib_counters
        )
        if [[ "${gpus_on_node}" == *"8 NVIDIA H100"* ]] || [[ "${gpus_on_node}" == *"8 NVIDIA H200"* ]]; then
            checks+=(
                hc_ib_link_state
                hc_ib_pkey
            )
        fi
    else
        echo "Skipping hc_* checks because there are no 8 GPUs"
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

exit 0
