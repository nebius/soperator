#!/bin/bash

set -eox pipefail

echo "[$(date)] Check Infiniband counters"

# Infiniband counters indicating errors (counters/ directory)
error_counters=(
    link_downed
    symbol_error
)
error_counters_pattern=$(IFS=\|; echo "${error_counters[*]}")

# Get non-zero error counters for all devices and ports
# Line format: "<device_name>:<port_num>:<counter_name>=<counter_value>"
counter_list=$(find -L /sys-host/class/infiniband -maxdepth 5 -path "*/ports/*/counters/*" -print0 2>/dev/null \
    | xargs -0 grep -H . \
    | sed -E 's|.*/infiniband/([^/]+)/ports/([0-9]+)/[^/]+/([^/]+):([0-9]+)$|\1:\2:\3=\4|' \
    | grep -E ":(?:$error_counters_pattern)=" | grep -v '=0$' || echo '')

# Check if there are any non-zero error counters
if [[ -n "$counter_list" ]]; then
    echo "Found non-zero error IB counters"

    first=$(echo "$counter_list" | head -n1 || true)

    # Return failure details
    echo "non-zero error IB counter ${first}" >&3
    exit 1
fi

exit 0
