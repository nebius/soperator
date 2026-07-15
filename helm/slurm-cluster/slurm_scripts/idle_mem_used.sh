#!/bin/bash

set -euo pipefail

max_used_gb="${IDLE_MEM_USED_MAX_USED_GB:-}"
meminfo_path="${IDLE_MEM_USED_MEMINFO_PATH:-/proc/meminfo}"
node_name="${SLURMD_NODENAME:-unknown}"

echo "[$(date)] Check memory usage when node ${node_name} is idle"
echo "Configured maximum used memory: ${max_used_gb:-<unset>} GB"
echo "Idle state source: local 'scontrol listjobs' (no controller RPC)"
echo "Idle state rule: exit code 1 with \"No slurmstepd's found on this node\" means the node has no local jobs"

if listjobs_output="$(scontrol listjobs 2>&1)"; then
    listjobs_rc=0
else
    listjobs_rc=$?
fi

echo "scontrol listjobs exit code: ${listjobs_rc}"
echo "scontrol listjobs output: ${listjobs_output:-<empty>}"

if (( listjobs_rc == 0 )); then
    node_is_idle=false
    echo "Local Slurm jobs are present; treating the node as non-idle"
elif (( listjobs_rc == 1 )) && [[ "${listjobs_output}" == *"No slurmstepd's found on this node"* ]]; then
    node_is_idle=true
    echo "No local slurmstepd was found; treating the node as idle"
else
    echo "Could not determine whether the node is idle from 'scontrol listjobs'; skipping memory validation" >&2
    exit 0
fi

echo "Node is idle: ${node_is_idle}"
if [[ "${node_is_idle}" != "true" ]]; then
    echo "Node has local jobs; skipping memory validation"
    exit 0
fi

if ! [[ "${max_used_gb}" =~ ^[0-9]+$ ]] || [[ "${max_used_gb}" == "0" ]]; then
    echo "Invalid idle memory threshold '${max_used_gb:-<unset>}'; expected a positive GB count, skipping memory validation" >&2
    exit 0
fi

max_used_bytes=$((max_used_gb * 1000000000))

meminfo_values="$(
    awk '
        /^MemTotal:/ { total = $2 }
        /^MemAvailable:/ { available = $2 }
        END { if (total != "" && available != "") print total, available }
    ' "${meminfo_path}" 2>/dev/null || true
)"
read -r mem_total_kib mem_available_kib <<< "${meminfo_values}"

if ! [[ "${mem_total_kib:-}" =~ ^[0-9]+$ ]] ||
   ! [[ "${mem_available_kib:-}" =~ ^[0-9]+$ ]] ||
   (( mem_available_kib > mem_total_kib )); then
    echo "Could not determine valid MemTotal and MemAvailable values from ${meminfo_path}; skipping memory validation" >&2
    exit 0
fi

mem_total_bytes=$((mem_total_kib * 1024))
mem_available_bytes=$((mem_available_kib * 1024))
mem_used_bytes=$((mem_total_bytes - mem_available_bytes))

mem_total_gb="$(awk -v bytes="${mem_total_bytes}" 'BEGIN { printf "%.2f", bytes / 1000000000 }')"
mem_available_gb="$(awk -v bytes="${mem_available_bytes}" 'BEGIN { printf "%.2f", bytes / 1000000000 }')"
mem_used_gb="$(awk -v bytes="${mem_used_bytes}" 'BEGIN { printf "%.2f", bytes / 1000000000 }')"

echo "Memory source: ${meminfo_path}"
echo "Memory measurements: total=${mem_total_gb} GB (${mem_total_bytes} bytes), available=${mem_available_gb} GB (${mem_available_bytes} bytes), used=total-available=${mem_used_gb} GB (${mem_used_bytes} bytes)"
echo "Maximum allowed used memory: ${max_used_gb} GB (${max_used_bytes} bytes)"

if (( mem_used_bytes > max_used_bytes )); then
    echo "Node ${node_name} is IDLE but uses ${mem_used_gb} GB of memory (threshold: ${max_used_gb} GB; MemTotal: ${mem_total_gb} GB; MemAvailable: ${mem_available_gb} GB). This may indicate leftover or spurious processes consuming memory." >&3
    exit 1
fi

echo "Idle node memory usage is within the configured threshold"
exit 0
