#!/bin/bash

set -eox pipefail

echo "[$(date)] Check if there are any pids running on allocated GPUs"

# If no GPUs were allocated by Slurm, nothing to do
if [[ -z "${SLURM_JOB_GPUS:-}" ]]; then
    echo "No GPU devices are requested by user" >&2
    exit 0
fi

# For each allocated GPU, check for running compute apps
pids=$(chroot /run/nvidia/driver /bin/bash -c "
  IFS=',' read -ra ALLOC_GPUS <<< \"\${SLURM_JOB_GPUS}\"
  for gpu in \"\${ALLOC_GPUS[@]}\"; do
      pid=\$(nvidia-smi \
          -i \"\${gpu}\" \
          --query-compute-apps='pid' \
          --format='csv,noheader' 2>/dev/null || echo '')
      if [[ -n \"\${pid}\" ]]; then
          echo \"\${pid}\"
      fi
  done
" | paste -sd ',' -)

if [[ -n "${pids}" ]]; then
    echo "Found PIDs running on allocated GPUs: ${pids}"

    # Return failure details
    echo "Allocated GPUs are used by processes not managed by Slurm, please stop them: PID $pids" >&3
    exit 1
fi

exit 0
