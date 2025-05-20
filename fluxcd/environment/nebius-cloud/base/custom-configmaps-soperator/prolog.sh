#!/bin/bash

echo "Cleanup leftover enroot containers if the job is restarted with the same ID"
containers=$(enroot list | grep -E "^pyxis_$SLURM_JOB_ID[._]")
if [ -n "$containers" ]; then
    for c in $containers; do
        enroot remove -f "$c"
    done
fi

if [ -n "$SLURM_JOB_GPUS" ]; then
    cd /tmp

    echo "Execute nvidia-smi health check"
    rm /tmp/nvidia-smi.out 2> /dev/null
    nvidia-smi 1> /tmp/nvidia-smi.out
    if [ $? -gt 0 ]; then
    scontrol update nodename=$SLURMD_NODENAME state=drain reason="Failed nvidia-smi, see /tmp/nvidia-smi.out"
    exit 0
    fi
fi

echo "Map the Slurm job with DCGM metrics"
metrics_dir="/var/run/nebius/slurm"

if [[ -z "${CUDA_VISIBLE_DEVICES:-}" ]]; then
    echo "No GPU devices are requested by user" >&2
    exit 0
fi

if [[ ! -d "$metrics_dir" ]]; then
    if ! mkdir -p "$metrics_dir"; then
        echo "Unable to create the data dir metrics_dir" >&2
        exit 0
    fi
fi

IFS=',' read -ra cuda_devs <<< "$CUDA_VISIBLE_DEVICES"

for gpu_id in "${cuda_devs[@]}"; do
    [[ -z "$gpu_id" ]] && continue
    echo "Writing $metrics_dir/$gpu_id"
    if ! printf "%s" "${SLURM_JOB_ID:-0}" > "$metrics_dir/$gpu_id"; then
        echo "Unable to write job file $metrics_dir/$gpu_id"
    fi
done
