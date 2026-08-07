#!/bin/bash

set -euo pipefail

# This recovery check must fail closed: only a fully validated healthy result
# may allow check_runner to undrain the node.
node_name="${SLURMD_NODENAME:-unknown}"
node_real_memory_bytes="${CHECKS_NODE_REAL_MEM_BYTES:-}"

echo "[$(date)] Check whether memory usage has recovered on drained node ${node_name}"
echo "Slurm RealMemory input: ${node_real_memory_bytes:-<unavailable>} bytes"
echo "Node eligibility source: this check is scheduled only for drained nodes"

if ! [[ "${node_real_memory_bytes}" =~ ^[0-9]+$ ]] || [[ "${node_real_memory_bytes}" == "0" ]]; then
    echo "Invalid or unavailable Slurm RealMemory '${node_real_memory_bytes:-<unavailable>}'; keeping node drained" >&2
    exit 1
fi

if ! free_bytes_output="$(LC_ALL=C free -b 2>&1)"; then
    echo "Could not read local memory information with 'free -b'; keeping node drained" >&2
    exit 1
fi

memory_values="$(awk '/^Mem:/ { print $2, $7; exit }' <<<"${free_bytes_output}")"
read -r mem_total_bytes mem_available_bytes <<<"${memory_values}"

if ! [[ "${mem_total_bytes:-}" =~ ^[0-9]+$ ]] ||
   ! [[ "${mem_available_bytes:-}" =~ ^[0-9]+$ ]] ||
   (( mem_available_bytes > mem_total_bytes )); then
    echo "Could not determine valid total and available memory from 'free -b'; keeping node drained" >&2
    exit 1
fi

if (( node_real_memory_bytes > mem_total_bytes )); then
    echo "Slurm RealMemory ${node_real_memory_bytes} bytes exceeds MemTotal ${mem_total_bytes} bytes; keeping node drained" >&2
    exit 1
fi

mem_available_gb="$(awk -v bytes="${mem_available_bytes}" 'BEGIN { printf "%.2f", bytes / 1000000000 }')"
node_real_memory_gb="$(awk -v bytes="${node_real_memory_bytes}" 'BEGIN { printf "%.2f", bytes / 1000000000 }')"

echo "Memory source: local 'free'"
echo "Memory comparison: available=${mem_available_gb} GB (${mem_available_bytes} bytes), Slurm RealMemory=${node_real_memory_gb} GB (${node_real_memory_bytes} bytes)"
echo "Memory snapshot (free -hw):"
if ! LC_ALL=C free -hw; then
    echo "Could not print the human-readable memory snapshot with 'free -hw'" >&2
fi

if (( mem_available_bytes < node_real_memory_bytes )); then
    echo "Available memory ${mem_available_gb} GB is still below configured memory ${node_real_memory_gb} GB; keeping node drained" >&2
    exit 1
fi

echo "Drained node leaves enough memory available for Slurm RealMemory; memory recovery confirmed"
exit 0
