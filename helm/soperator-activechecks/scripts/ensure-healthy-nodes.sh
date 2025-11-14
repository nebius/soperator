#!/bin/bash
#SBATCH --deadline="now+4hours"
#SBATCH --time=5:00

set -euo pipefail

json=$(scontrol show nodes --json)
bad_nodes=$(echo "$json" | jq -r '
  .nodes[]
  | select(
      (.reason // "") != "" 
      or (.comment // "") != "" 
      or ((.state? // "") | tostring | test("DOWN|DRAIN|FAIL"))
    )
  | {name, reason: (.reason // ""), comment: (.comment // ""), state: .state}
')

if [[ -n "$bad_nodes" ]]; then
  echo "Found non-healthy nodes: $bad_nodes"
  exit 1
else
  echo "All nodes are healthy"
fi
