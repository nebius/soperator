#!/bin/bash

set -euxo pipefail

# Runs once at the job beginning without `SLURM_STEP_ID` and for each step with `SLURM_STEP_ID`.

if [ "${SLURM_LOCALID}" = "0" ]; then
    echo export "DOCKER_HOST=unix:///var/run/soperator-docker.sock"

    export CHECKS_OUTPUTS_BASE_DIR="/opt/soperator-outputs/local"
    task_prolog="$CHECKS_OUTPUTS_BASE_DIR/task_prolog"
    (umask 000; mkdir -p "$task_prolog")
    (
        printf "Slurm environment variables for task prolog:\n"
        env | grep -E '^(SLURM_|SLURMD_|SRUN_|SBATCH_)' || true
        printf "\n"
    ) > "$task_prolog/$SLURMD_NODENAME.$SLURM_JOB_ID.${SLURM_STEP_ID:-}.${SLURM_ARRAY_TASK_ID:-}.out"
fi

if [ -v SLURM_JOB_ID_SOPERATOR_TASK_PROLOG ] && [ "${SLURM_JOB_ID_SOPERATOR_TASK_PROLOG:-}" != "${SLURM_JOB_ID:-}" ] && [ -z "${SOPERATOR_SUPPRESS_RECURSIVE_SRUN:-}" ]; then
    echo print "A job submission from within another job was detected. If this behavior is intentional, set SOPERATOR_SUPPRESS_RECURSIVE_SRUN=1."
fi

if [ -v SLURM_JOB_ID ]; then
    echo export "SLURM_JOB_ID_SOPERATOR_TASK_PROLOG=${SLURM_JOB_ID:-}"
fi

{{- with .Values.slurmScripts.taskPrologEnv }}
{{- if .enabled }}

for env_file in{{ range .sourceFiles }} {{ . | quote }}{{ end }}; do
    if [ ! -r "$env_file" ]; then
        echo print "Skipping unreadable task prolog env file: $env_file"
        continue
    fi

    # shellcheck disable=SC1090
    if ! source "$env_file"; then
        echo print "Failed to source task prolog env file: $env_file"
        continue
    fi
done

for env_var in{{ range .exportVars }} {{ . | quote }}{{ end }}; do
    case "$env_var" in
        ''|[0-9]*|*[!A-Za-z0-9_]*)
            echo print "Ignoring invalid task prolog env var name: $env_var"
            continue
            ;;
    esac

    env_value="${!env_var:-}"
    if [ -n "$env_value" ]; then
        echo export "${env_var}=${env_value}"
    fi
done
{{- end }}
{{- end }}
