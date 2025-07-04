#!/bin/bash

set -eox

echo "[$(date)] Cleanup leftover enroot containers if the job is restarted with the same ID"

containers=$(enroot list | grep -E "^pyxis_$SLURM_JOB_ID[._]")
if [ -n "$containers" ]; then
    for c in $containers; do
        enroot remove -f "$c"
    done
fi
