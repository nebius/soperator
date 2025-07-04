#!/bin/bash

set -eox

echo "[$(date)] Map the Slurm job with DCGM metrics"

# set in hpcJobMapDir in soperator/helm/soperator-fluxcd/values.yaml
#   and dcgm_job_map_dir in nebius-solution-library/soperator/modules/slurm/variables.tf
#   check those before changing it here
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
