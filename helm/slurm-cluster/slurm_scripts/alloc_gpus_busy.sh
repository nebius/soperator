#!/bin/bash

set -eox pipefail

echo "[$(date)] Check if there are any processes running on allocated GPUs"

# If no GPUs were allocated by Slurm, nothing to do
if [[ -z "${SLURM_JOB_GPUS:-}" ]]; then
    echo "No GPU devices are requested by user" >&2
    exit 0
fi

# Build up an array of "-i <gpu_index>" arguments for nvidia-smi
# SLURM_JOB_GPUS is a comma-separated list of GPU indices, e.g. "0,2"
IFS=',' read -ra _gpus <<< "${SLURM_JOB_GPUS}"
nvidia_args=()
for gpu in "${_gpus[@]}"; do
    nvidia_args+=( -i "${gpu}" )
done

processes=$(chroot /run/nvidia/driver nvidia-smi \
    "${nvidia_args[@]}" \
    --query-compute-apps="gpu_serial,process_name" \
    --format="csv,noheader" || echo '')

if [[ -n "${processes}" ]]; then
    echo "Found processes running on GPUs"
    echo "${processes}"

    # Return failure details
    echo "GPUs are in use by processes not managed by Slurm" >&3
    exit 1
fi

exit 0
