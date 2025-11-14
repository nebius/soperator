#!/bin/bash

set -euxo pipefail

echo "[$(date)] Check if no process are running on GPUs"

# Check if chroot to driver dir required
chroot_cmd=""
driver_dir="/run/nvidia/driver"
if [[ "$(ls -A $driver_dir)" ]]; then
    chroot_cmd="chroot $driver_dir"
fi

out=$($chroot_cmd nvidia-smi \
    --query-compute-apps="gpu_serial,process_name,pid" \
    --format="csv,noheader" 2>/dev/null || echo "")
if [[ -n "${out}" ]]; then
    echo "Found processes running on GPUs:"
    echo "$out"
    exit 1
fi

echo "No processes running on GPUs"
exit 0
