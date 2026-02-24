#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code

feature_conf() {
    local current_host
    if ! current_host=$(hostname); then
        echo "Error: Failed to determine hostname" >&2
        return 1
    fi
    local features=()

    for var in $(env | grep "^SLURM_FEATURE_" | cut -d= -f1); do
        local feature_name=${var#SLURM_FEATURE_}
        local hostlist_expr=${!var}

        local expanded_hosts
        if ! expanded_hosts=$(scontrol show hostnames "$hostlist_expr"); then
            echo "Warning: Failed to expand hostlist expression '$hostlist_expr' for feature '$feature_name'" >&2
            continue
        fi

        if echo "$expanded_hosts" | grep -q "^$current_host$"; then
            features+=("$feature_name")
        fi
    done

    if [ ${#features[@]} -ne 0 ]; then
        local IFS=","
        echo " Feature=${features[*]} "
    fi
}

TOPO_LABELS_FILE="/tmp/slurm/topology-node-labels"
if [[ -f $TOPO_LABELS_FILE ]]; then
    switch_tier2=$(jq -r '."tier-2" // empty' "$TOPO_LABELS_FILE" 2>/dev/null || echo "")
    export TOPO_SWITCH_TIER2="${switch_tier2:-unknown}"
else
    export TOPO_SWITCH_TIER2="unknown"
fi

echo "Evaluate variables in the Slurm node 'Extra' field"
evaluated_extra=$(eval echo "$SLURM_NODE_EXTRA")

echo "Start slurmd daemon"

slurmd_args=(
  -D
  --instance-id "${INSTANCE_ID}"
  -b
)

if [ "${evaluated_extra}" != "" ]; then
  slurmd_args+=(
    --extra "${evaluated_extra}"
  )
fi

exec /usr/sbin/slurmd "${slurmd_args[@]}" 2>&1 | tee >(multilog s100000000 n5 /var/log/slurm/multilog)
