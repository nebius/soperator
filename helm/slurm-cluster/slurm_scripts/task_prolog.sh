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

task_prolog_env_source_files=({{ range .sourceFiles }} {{ . | quote }}{{ end }})
task_prolog_env_export_vars=({{ range .exportVars }} {{ . | quote }}{{ end }})
task_prolog_env_vars_to_export=()

for env_var in "${task_prolog_env_export_vars[@]}"; do
    case "$env_var" in
        ''|[0-9]*|*[!A-Za-z0-9_]*)
            printf 'Ignoring invalid task prolog env var name: %s\n' "$env_var" >&2
            continue
            ;;
    esac

    if [ -n "${!env_var+x}" ]; then
        continue
    fi

    task_prolog_env_vars_to_export+=("$env_var")
done

if [ "${#task_prolog_env_vars_to_export[@]}" -gt 0 ]; then
    if task_prolog_env_output="$(
        set +eu

        for env_file in "${task_prolog_env_source_files[@]}"; do
            if [ ! -r "$env_file" ]; then
                continue
            fi

            # shellcheck disable=SC1090
            source "$env_file" >/dev/null 2>&1 || true
        done

        for env_var in "${task_prolog_env_vars_to_export[@]}"; do
            env_value="${!env_var-}"
            if [ -z "$env_value" ]; then
                continue
            fi
            case "$env_value" in
                *$'\n'*|*$'\r'*)
                    printf 'Ignoring task prolog env var %s with multiline value\n' "$env_var" >&2
                    continue
                    ;;
            esac

            printf '%s=%s\n' "$env_var" "$env_value"
        done
    )"; then
        while IFS= read -r env_line; do
            env_var="${env_line%%=*}"
            env_value="${env_line#*=}"
            if [ "$env_var" = "$env_line" ]; then
                continue
            fi

            printf 'export %s=%s\n' "$env_var" "$env_value"
        done <<< "$task_prolog_env_output"
    else
        printf 'Failed to load task prolog env files\n' >&2
    fi
fi

unset task_prolog_env_output
unset task_prolog_env_vars_to_export
unset task_prolog_env_export_vars
unset task_prolog_env_source_files
{{- end }}
{{- end }}
