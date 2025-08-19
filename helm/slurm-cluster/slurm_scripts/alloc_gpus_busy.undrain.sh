#!/bin/bash

set -euxo pipefail

echo "[$(date)] Check if no process are running on GPUs"

out=$(chroot /run/nvidia/driver nvidia-smi \
    --query-compute-apps="gpu_serial,process_name,pid" \
    --format="csv,noheader" 2>/dev/null || echo "")
if [[ -n "${out}" ]]; then
    echo "Found processes running on GPUs:"
    echo "$out"
    exit 1
fi

echo "No processes running on GPUs"
exit 0
