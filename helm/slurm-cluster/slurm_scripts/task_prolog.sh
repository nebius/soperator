#!/bin/bash

set -euxo pipefail

# Runs once at the job beginning without `SLURM_STEP_ID` and for each step with `SLURM_STEP_ID`.

if [ "${SLURM_LOCALID}" = "0" ]; then
    export CHECKS_OUTPUTS_BASE_DIR="/opt/soperator-outputs"
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

if ! /usr/bin/python3 -c "import sys; sys.exit(0)" >/dev/null 2>&1; then
    echo "Python is not installed or not working" >&2
    exit 0
fi

if [ -z "${SLURM_JOB_ID:-}" ]; then
    exit 0
fi

proxy_dir="/tmp/soperator-docker-host"
proxy_prefix="${SLURMD_NODENAME}.${SLURM_JOB_ID}.${SLURM_STEP_ID:-}.${SLURM_ARRAY_TASK_ID:-}"
proxy_socket="$proxy_dir/$proxy_prefix.sock"
proxy_pid_file="$proxy_dir/$proxy_prefix.pid"
proxy_log_file="$proxy_dir/$proxy_prefix.log"

if [ "${SLURM_LOCALID}" = "0" ]; then
    (umask 000; mkdir -p "$proxy_dir")

    if [ -f "$proxy_pid_file" ] && kill -0 "$(cat "$proxy_pid_file")" >/dev/null 2>&1; then
        echo "Docker host proxy is already running for $proxy_prefix"
    else
        rm -f "$proxy_socket" "$proxy_pid_file"
        nohup /usr/bin/python3 /opt/slurm_scripts/docker_host_proxy.py \
            --listen-socket "$proxy_socket" \
            --target-socket /var/run/docker.sock \
            >"$proxy_log_file" 2>&1 < /dev/null &
        echo "$!" > "$proxy_pid_file"
        chmod 0666 "$proxy_pid_file" || true
    fi
fi

for _ in $(seq 1 100); do
    if [ -S "$proxy_socket" ]; then
        break
    fi
    sleep 0.05
done

echo export "DOCKER_HOST=unix://$proxy_socket"

if [ ! -S "$proxy_socket" ]; then
    echo "Docker host proxy socket was not created: $proxy_socket" >&2
fi
