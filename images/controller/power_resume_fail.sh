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
/opt/soperator/bin/power-manager suspend -nodes "$1"
exit_code=$?

if [ $exit_code -ne 0 ]; then
    log_json "error" "ResumeFailProgram suspend failed" ",\"exit_code\":${exit_code}"
    exit $exit_code
fi

# Wait for nodes to be removed from activeNodes (verify the update was applied)
/opt/soperator/bin/power-manager wait-removed -nodes "$1" -timeout 180s
exit_code=$?

if [ $exit_code -ne 0 ]; then
    log_json "error" "ResumeFailProgram wait-removed failed" ",\"exit_code\":${exit_code}"
fi

exit $exit_code
