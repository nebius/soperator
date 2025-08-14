#!/bin/bash

set -euxo pipefail

echo "[$(date)] Cleanup leftover enroot containers if the job is restarted with the same ID"

if [[ -z "${SLURM_JOB_ID:-}" ]]; then
    echo "Slurm job ID is not known" >&2
    exit 0
fi

containers=$(enroot list | grep -E "^pyxis_${SLURM_JOB_ID}[._]" || echo "")
if [ -n "$containers" ]; then
    for c in $containers; do
        enroot remove -f "$c" || true
    done
fi
