#!/bin/bash
#SBATCH --deadline="now+4hours"
#SBATCH --time=5:00

set -euo pipefail

json=$(scontrol show nodes --json)
bad_nodes=$(echo "$json" | jq -r '
  .nodes[]
  | select(
      (.reason // "") != ""
      or ([.state] | flatten | any(IN("DOWN","DRAIN","FAIL","ERROR","INVALID_REG")))
    )
  | {name, reason: (.reason // ""), state: .state}
')

if [[ -n "$bad_nodes" ]]; then
  echo "Found non-healthy nodes: $bad_nodes"
  exit 1
else
  echo "All nodes are healthy"
fi
