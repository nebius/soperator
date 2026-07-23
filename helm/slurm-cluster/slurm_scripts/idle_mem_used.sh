#!/bin/bash

set -euo pipefail

node_name="${SLURMD_NODENAME:-unknown}"
node_real_memory_bytes="${CHECKS_NODE_REAL_MEM_BYTES:-}"

echo "[$(date)] Check memory usage when node ${node_name} is idle"
echo "Slurm RealMemory input: ${node_real_memory_bytes:-<unavailable>} bytes"
echo "Idle state source: local 'scontrol listjobs --json' (no controller RPC)"
echo "Idle state rule: an empty JSON .jobs array means the node has no local jobs"

if listjobs_output="$(scontrol listjobs --json 2>&1)"; then
    listjobs_rc=0
else
    listjobs_rc=$?
fi

echo "scontrol listjobs --json exit code: ${listjobs_rc}"
echo "scontrol listjobs --json output: ${listjobs_output:-<empty>}"

if (( listjobs_rc != 0 )); then
    echo "Could not determine whether the node is idle because 'scontrol listjobs --json' failed; skipping memory validation" >&2
    exit 0
fi

if ! local_job_count="$(
    jq -er '
        if (.jobs | type) == "array" then
            .jobs | length
        else
            error(".jobs must be an array")
        end
    ' <<<"${listjobs_output}" 2>/dev/null
)"; then
    echo "Could not determine whether the node is idle because 'scontrol listjobs --json' returned invalid job data; skipping memory validation" >&2
    exit 0
fi

echo "Local Slurm job count from JSON .jobs array: ${local_job_count}"
if (( local_job_count == 0 )); then
    node_is_idle=true
    echo "The JSON .jobs array is empty; treating the node as idle"
else
    node_is_idle=false
    echo "The JSON .jobs array contains local jobs; treating the node as non-idle"
fi

echo "Node is idle: ${node_is_idle}"
if [[ "${node_is_idle}" != "true" ]]; then
    echo "Node has local jobs; skipping memory validation"
    exit 0
fi

if ! [[ "${node_real_memory_bytes}" =~ ^[0-9]+$ ]] || [[ "${node_real_memory_bytes}" == "0" ]]; then
    echo "Invalid or unavailable Slurm RealMemory '${node_real_memory_bytes:-<unavailable>}'; expected a positive byte count, skipping memory validation" >&2
    exit 0
fi

if ! free_bytes_output="$(LC_ALL=C free -b 2>&1)"; then
    echo "Could not read local memory information with 'free -b'; skipping memory validation" >&2
    exit 0
fi

memory_values="$(awk '/^Mem:/ { print $2, $7; exit }' <<<"${free_bytes_output}")"
read -r mem_total_bytes mem_available_bytes <<<"${memory_values}"

if ! [[ "${mem_total_bytes:-}" =~ ^[0-9]+$ ]] ||
   ! [[ "${mem_available_bytes:-}" =~ ^[0-9]+$ ]] ||
   (( mem_available_bytes > mem_total_bytes )); then
    echo "Could not determine valid total and available memory from 'free -b'; skipping memory validation" >&2
    exit 0
fi

if (( node_real_memory_bytes > mem_total_bytes )); then
    echo "Slurm RealMemory ${node_real_memory_bytes} bytes exceeds MemTotal ${mem_total_bytes} bytes; skipping memory validation" >&2
    exit 0
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
    echo "available memory ${mem_available_gb} GB < configured ${node_real_memory_gb} GB; stop leftover processes or reboot" >&3
    exit 1
fi

echo "Idle node leaves enough memory available for Slurm RealMemory"
exit 0
