#!/bin/bash

set -e

# Run GPU healthcheck
exit_code=0
output=$(/usr/bin/nvidia-smi 2>&1) || exit_code=$?

current_node=$(hostname)
node_info=$(scontrol show node "$current_node")
node_status=$(sinfo -h -n "$current_node" -o "%t")
node_reason=$(echo "$node_info" | grep "Reason=" | awk -F'Reason=' '{print $2}')

if [[ $exit_code -eq 0 ]];
  then
    echo "OK"
    if [[ "$node_status" == "drain" && "$node_reason" == "GPUHealthcheckError" ]];
      then
        scontrol update NodeName="$current_node" State=resume Reason=""
    fi
else
  echo "ERROR: nvidia-smi finished with exit code $exit_code"
  echo "$output"
  scontrol update NodeName="$current_node" State=drain Reason="GPUHealthcheckError"
fi
