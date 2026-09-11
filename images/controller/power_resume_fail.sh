#!/bin/bash
# Slurm ResumeFailProgram for ephemeral nodes
# This script is called by slurmctld when nodes fail to resume within ResumeTimeout
# It calls power-manager which removes the ordinals from NodeSetPowerState CRs,
# so the worker pods that did not become ready in time are torn down

log_json() {
    local level="$1"
    local msg="$2"
    local extra="$3"
    echo "{\"time\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",\"logger\":\"power_resume_fail\",\"level\":\"${level}\",\"msg\":\"${msg}\"${extra}}"
}

log_json "info" "ResumeFailProgram invoked" ",\"script\":\"$0\",\"nodes\":\"$1\""

# Call power-manager to power the nodes back down
# $1 contains the node list in Slurm format (e.g., "worker-[0-5,7]")
exec /opt/soperator/bin/power_action.sh suspend "$1"
