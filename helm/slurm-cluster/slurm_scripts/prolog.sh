#!/bin/bash

set -euxo pipefail

if ! /usr/bin/python3 -c "import sys; sys.exit(0)" >/dev/null 2>&1; then
    echo "Python is not installed or not working" >&2
    exit 0
fi

export CHECKS_CONTEXT="prolog"
export CHECKS_CONFIG="/opt/slurm_scripts/checks.json"
export CHECKS_OUTPUTS_BASE_DIR="/opt/soperator-outputs"
export CHECKS_RUNNER_OUTPUT="/mnt/jail$CHECKS_OUTPUTS_BASE_DIR/slurm_scripts/$SLURMD_NODENAME.check_runner.$CHECKS_CONTEXT.out"
{{ if .Values.slurmScripts.scontrolAudit.enabled }}
export SCONTROL_AUDIT_ENABLED="1"
export SCONTROL_AUDIT_LOG={{ .Values.slurmScripts.scontrolAudit.logPath | quote }}
export SCONTROL_AUDIT_REAL_SCONTROL={{ .Values.slurmScripts.scontrolAudit.realScontrolPath | quote }}
export PATH="/opt/slurm_scripts:$PATH"
{{ else }}
export SCONTROL_AUDIT_ENABLED="0"
export PATH="$PATH"
{{ end }}

echo "Starting check_runner.py"
/usr/bin/python3 /opt/slurm_scripts/check_runner.py 2>&1
