#!/bin/bash

set -eox pipefail

echo "[$(date)] Check if there are any processes running on allocated GPUs"

# If no GPUs were allocated by Slurm, nothing to do
if [[ -z "${SLURM_JOB_GPUS:-}" ]]; then
    echo "No GPU devices are requested by user" >&2
    exit 0
fi

# Split the comma-separated list of allocated GPU indices into an array
IFS=',' read -ra ALLOC_GPUS <<< "${SLURM_JOB_GPUS}"

processes=""
# For each allocated GPU, check for running compute apps
for gpu in "${ALLOC_GPUS[@]}"; do
    out=$(chroot /run/nvidia/driver nvidia-smi \
        -i "${gpu}" \
        --query-compute-apps="gpu_serial,process_name,pid,used_memory" \
        --format="csv,noheader" 2>/dev/null || echo "")
    if [[ -n "${out}" ]]; then
        processes+=$'\n'"GPU ${gpu}: ${out}"
    fi
done

if [[ -n "${processes}" ]]; then
    echo "Found processes running on allocated GPUs: ${processes}"

    # Return failure details
    echo "GPUs are in use by processes not managed by Slurm" >&3
    exit 1
fi

exit 0
