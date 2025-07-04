#!/bin/bash

set -eox pipefail

export LOGS_OUTPUT_DIR="/var/spool/slurmd/soperator-outputs/${SLURMD_NODENAME}/slurm_scripts"
export SCRIPT_CONTEXT="hc_program"

(umask 000; mkdir -p ${LOGS_OUTPUT_DIR})

echo "Execute GPU healthchecks"
chroot /mnt/jail /bin/bash -s <<-'EOF'
    set -eox pipefail

    # The list of healthchecks in the execution order
    checks=(
        health_checker
    )

    pushd /opt/slurm_scripts || exit 0
    for check in "${checks[@]}"; do
        script="${check}.sh"
        log="${LOGS_OUTPUT_DIR}/${check}.${SCRIPT_CONTEXT}.out"

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