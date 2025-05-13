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
