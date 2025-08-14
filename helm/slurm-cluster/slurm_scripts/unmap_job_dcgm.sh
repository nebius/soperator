#!/bin/bash

set -euxo pipefail

echo "[$(date)] Unmap the Slurm job with DCGM metrics"

# set in hpcJobMapDir in soperator/helm/soperator-fluxcd/values.yaml
#   and dcgm_job_map_dir in nebius-solution-library/soperator/modules/slurm/variables.tf
#   check those before changing it here
metrics_dir="/var/run/nebius/slurm"

if [[ -z "${SLURM_JOB_GPUS:-}" ]]; then
    echo "No GPU devices are requested by user" >&2
    exit 0
fi

IFS=',' read -ra cuda_devs <<< "$SLURM_JOB_GPUS"

for gpu_id in "${cuda_devs[@]}"; do
    [[ -z "$gpu_id" ]] && continue
    echo "Removing $metrics_dir/${gpu_id:-99}"
    rm -f "${metrics_dir:-}/${gpu_id:-99}" || echo "Unable to remove file ${metrics_dir:-}/${gpu_id:-99}"
done
