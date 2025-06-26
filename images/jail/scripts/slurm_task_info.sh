#!/bin/bash

# This script prints various details about the resources the Slurm task is bound to.
# Intended to be used as a Slurm task prolog: srun --task-prolog=<path_to_this_script>

printf "print SLURM_TASK_INFO node=%s rank=%s cpu=%s gpu=%s cuda_dev=%s\n" \
    "$SLURMD_NODENAME" \
    "$SLURM_PROCID" \
    "$(cat /proc/self/status | grep "Cpus_allowed_list:" | sed "s/Cpus_allowed_list:\t//g")" \
    "$(nvidia-smi --query-gpu=pci.bus_id --format=csv,noheader | tr -d 0:. | paste -sd,)" \
    "$CUDA_VISIBLE_DEVICES"
