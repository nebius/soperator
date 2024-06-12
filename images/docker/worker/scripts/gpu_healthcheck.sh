#!/bin/bash

set -e

# Run GPU healthcheck
/usr/bin/nvidia-smi
exit_code=$?

current_node=$(hostname)
node_info=$(scontrol show node "$current_node")
node_status=$(sinfo -h -n "$current_node" -o "%t")
node_reason=$(echo "$node_info" | grep "Reason=" | awk -F'Reason=' '{print $2}')

if [[ $exit_code -eq 0 ]];
  then
    if [[ "$node_status" == "drain" && "$node_reason" == "GPUProblem" ]];
      then
        scontrol update NodeName="$current_node" State=resume Reason="Undraining after GPUProblem"
    fi
else
  scontrol update NodeName="$current_node" State=drain Reason="GPUProblem"
fi
