#!/bin/bash

set -euxo pipefail

echo "[$(date)] Check memory usage"

sys_available_mem=$(free -b | awk '/^Mem:/ {print $7}')
if ! [[ "$sys_available_mem" =~ ^[0-9]+$ ]]; then
    echo "Could not determine available memory on this node, exiting"
    exit 0
fi
echo "System available memory: $sys_available_mem"

node_real_mem="$CHECKS_NODE_REAL_MEM_BYTES"
if [ -z "$node_real_mem" ] || [ "$node_real_mem" == "0" ]; then
    echo "No info about the node real memory, exiting"
    exit 0
fi
echo "Node real memory: $node_real_mem"

if [[ $node_real_mem -gt $sys_available_mem ]]; then
    echo "Not enough available memory on the node" >&3
    exit 1
fi

echo "Enough available memory on the node"
exit 0
