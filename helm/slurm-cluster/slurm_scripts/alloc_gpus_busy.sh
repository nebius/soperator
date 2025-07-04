#!/bin/bash

set -eox pipefail

echo "[$(date)] Check if there are any processes running on allocated GPUs"

if [[ -z "${CUDA_VISIBLE_DEVICES:-}" ]]; then
    echo "No GPU devices are requested by user" >&2
    exit 0
fi

processes=$(chroot /run/nvidia/driver nvidia-smi --query-compute-apps="gpu_serial,process_name" --format="csv,noheader" || echo '')

if [[ -n "${processes}" ]]; then
    echo "Found processes running on GPUs"

    # Return failure details
    echo "GPUs are in use by processes not managed by Slurm" >&3
    exit 1
fi

exit 0
