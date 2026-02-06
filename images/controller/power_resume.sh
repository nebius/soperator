#!/bin/bash
# Slurm ResumeProgram for ephemeral nodes
# This script is called by slurmctld when nodes need to be resumed/powered on
# It calls power-manager which updates NodeSetPowerState CRs

log_json() {
    local level="$1"
    local msg="$2"
    local extra="$3"
    echo "{\"time\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",\"logger\":\"power_resume\",\"level\":\"${level}\",\"msg\":\"${msg}\"${extra}}"
}

log_json "info" "ResumeProgram invoked" ",\"script\":\"$0\",\"nodes\":\"$1\""

# Call power-manager to resume the nodes
# $1 contains the node list in Slurm format (e.g., "worker-[0-5,7]")
/opt/soperator/bin/power-manager resume -nodes "$1"
exit_code=$?

if [ $exit_code -ne 0 ]; then
    log_json "error" "ResumeProgram resume failed" ",\"exit_code\":${exit_code}"
    exit $exit_code
fi

# Wait for nodes to appear in activeNodes (verify the update was applied)
/opt/soperator/bin/power-manager wait-added -nodes "$1" -timeout 180s
exit_code=$?

if [ $exit_code -ne 0 ]; then
    log_json "error" "ResumeProgram wait failed" ",\"exit_code\":${exit_code}"
fi

exit $exit_code
