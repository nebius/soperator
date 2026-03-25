#!/bin/bash

set -euxo pipefail

if [ -z "${SLURM_JOB_ID:-}" ]; then
    exit 0
fi

proxy_prefix="docker-proxy-${SLURMD_NODENAME}.${SLURM_JOB_ID}.${SLURM_STEP_ID:-}.${SLURM_ARRAY_TASK_ID:-}"
proxy_socket="/var/run/$proxy_prefix.sock"
proxy_pid_file="/var/run/$proxy_prefix.pid"

if [ "${SLURM_LOCALID:-}" != "0" ]; then
    exit 0
fi

if [ ! -f "$proxy_pid_file" ]; then
    rm -f "$proxy_socket"
    exit 0
fi

proxy_pid="$(cat "$proxy_pid_file")"

if kill -0 "$proxy_pid" >/dev/null 2>&1; then
    kill "$proxy_pid" || true
    for _ in $(seq 1 100); do
        if ! kill -0 "$proxy_pid" >/dev/null 2>&1; then
            break
        fi
        sleep 0.05
    done
fi

rm -f "$proxy_pid_file" "$proxy_socket"
