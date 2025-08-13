#!/bin/bash

set -euxo pipefail

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
")

if [[ -n "${pids}" ]]; then
    echo "Found PIDs running on allocated GPUs:"
    echo "$pids"

    # Return failure details
    negative_pids=$(echo "$pids" | sed 's/^/-/g' | tr '\n' ' ')
    echo "Allocated GPUs are used by processes not managed by Slurm. \
Stop them using 'ssh $SLURMD_NODENAME kill -- \"$negative_pids\"', \
reboot the node using 'scontrol reboot $SLURMD_NODENAME', \
or stop-start the InstanceId from 'scontrol show node $SLURMD_NODENAME'" >&3
    exit 1
fi

exit 0
