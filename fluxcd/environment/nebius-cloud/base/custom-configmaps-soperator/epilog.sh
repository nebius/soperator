#!/bin/bash

if [ -n "$SLURM_JOB_GPUS" ]; then
    cd /tmp

    echo "Execute nvidia-smi health check"
    rm /tmp/nvidia-smi.out 2> /dev/null
    nvidia-smi 1> /tmp/nvidia-smi.out
    if [ $? -gt 0 ]; then
    scontrol update nodename=$SLURMD_NODENAME state=drain reason="Failed nvidia-smi, see /tmp/nvidia-smi.out"
    exit 0
    fi

    echo "Execute dcgm health check"
    rm /tmp/dcgm.out 2> /dev/null
    chroot /mnt/jail dcgmi diag -r 1 1> /tmp/dcgm.out
    grep -i Fail /tmp/dcgm.out > /dev/null
    if [ $? -eq 0 ]; then
    scontrol update nodename=$SLURMD_NODENAME state=drain reason="Failed DCGM, see /tmp/dcgm.out"
    exit 0
    fi
fi

echo "Unmap the Slurm job with DCGM metrics"
# set in hpcJobMapDir in soperator/helm/soperator-fluxcd/values.yaml
#   and dcgm_job_map_dir in nebius-solution-library/soperator/modules/slurm/variables.tf
#   check those before changing it here
metrics_dir="/var/run/nebius/slurm"

if [[ -z "${CUDA_VISIBLE_DEVICES:-}" ]]; then
    echo "No GPU devices are requested by user" >&2
    exit 0
fi

IFS=',' read -ra cuda_devs <<< "$CUDA_VISIBLE_DEVICES"

for gpu_id in "${cuda_devs[@]}"; do
    [[ -z "$gpu_id" ]] && continue
    echo "Removing $metrics_dir/${gpu_id:-99}"
    rm -f "${metrics_dir:-}/${gpu_id:-99}" || echo "Unable to remove file ${metrics_dir:-}/${gpu_id:-99}"
done
