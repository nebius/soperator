#!/bin/bash

set -euxo pipefail

echo "[$(date)] Check memory usage"

sys_available_mem=$(free -b | awk '/^Mem:/ {print $7}')
if ! [[ "$sys_available_mem" =~ ^[0-9]+$ ]]; then
    echo "Could not determine available memory on this node, exiting"
    exit 0
fi
echo "System available memory: $sys_available_mem"

job_allocated_mem="$CHECKS_JOB_ALLOC_MEM_BYTES"
if [ -z "$job_allocated_mem" ] || [ "$job_allocated_mem" == "0" ]; then
    echo "No info about the memory allocated for the job, exiting"
    exit 0
fi
echo "Job allocated memory: $job_allocated_mem"

if [[ $job_allocated_mem -gt $sys_available_mem ]]; then
    echo "Not enough available memory on the node"
    job_allocated_mem_gib=$(awk "BEGIN {printf \"%.2f\", $job_allocated_mem / (1024*1024*1024)}")
    sys_available_mem_gib=$(awk "BEGIN {printf \"%.2f\", $sys_available_mem / (1024*1024*1024)}")

    # Return failure details
    echo "Job ${SLURM_JOB_ID} allocated ${job_allocated_mem_gib}GiB of memory, \
but only ${sys_available_mem_gib} is available in the system. \
Clean up volumes from 'ssh $SLURMD_NODENAME /opt/soperator_utils/fs_usage.sh -m', \
reboot the node using 'scontrol reboot $SLURMD_NODENAME', \
or stop-start the InstanceId from 'scontrol show node $SLURMD_NODENAME'" >&3
    exit 1
fi

echo "Enough available memory on the node"
exit 0
