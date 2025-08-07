#!/bin/bash

export CHECKS_OUTPUTS_BASE_DIR="/opt/soperator-outputs"
export CHECKS_CONTEXT="prolog"
export CHECKS_CONFIG="/opt/slurm_scripts/checks.json"

out_dir="/mnt/jail$CHECKS_OUTPUTS_BASE_DIR/slurm_scripts"
out_file="$out_dir/$SLURMD_NODENAME.check_runner.$CHECKS_CONTEXT.out"
(umask 000; mkdir -p "$out_dir")

echo "Starting check_runner.py"
/usr/bin/python3 /opt/slurm_scripts/check_runner.py > "$out_file" 2>&1
