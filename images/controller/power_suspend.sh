#!/bin/bash
# Slurm SuspendProgram for ephemeral nodes
# This script is called by slurmctld when nodes need to be suspended/powered off
# It calls power-manager which updates NodeSetPowerState CRs

log_json() {
    local level="$1"
    local msg="$2"
    local extra="$3"
    echo "{\"time\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",\"logger\":\"power_suspend\",\"level\":\"${level}\",\"msg\":\"${msg}\"${extra}}"
}

log_json "info" "SuspendProgram invoked" ",\"script\":\"$0\",\"nodes\":\"$1\""

# Call power-manager to suspend the nodes
# $1 contains the node list in Slurm format (e.g., "worker-[0-5,7]")
exec /opt/soperator/bin/power_action.sh suspend "$1"
