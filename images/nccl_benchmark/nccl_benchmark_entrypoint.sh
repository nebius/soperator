#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code

echo "Link users from jail"
ln -s /mnt/jail/etc/passwd /etc/passwd
ln -s /mnt/jail/etc/group /etc/group
ln -s /mnt/jail/etc/shadow /etc/shadow
ln -s /mnt/jail/etc/gshadow /etc/gshadow
chown -h 0:42 /etc/{shadow,gshadow}

echo "Bind-mount slurm configs from K8S config map"
for file in /mnt/slurm-configs/*; do
    filename=$(basename "$file")
    touch "/etc/slurm/$filename" && mount --bind "$file" "/etc/slurm/$filename"
done

echo "Bind-mount munge key from K8S secret"
mount --bind /mnt/munge-key/munge.key /etc/munge/munge.key

echo "Starting munge"
munged --num-threads="$MUNGE_NUM_THREADS" --key-file="$MUNGE_KEY_FILE" --pid-file="$MUNGE_PID_FILE" -S "$MUNGE_SOCKET_FILE"

# TODO: Since 1.29 kubernetes supports native sidecar containers. We can remove it in feature releases
echo "Waiting until munge started"
while [ ! -S "/run/munge/munge.socket.2" ]; do sleep 2; done

echo "Start NCCL test benchmark"
/usr/bin/srun_perf.sh \
    -b "$NCCL_MIN_BYTES" \
    -e "$NCCL_MAX_BYTES" \
    -f "$NCCL_STEP_FACTOR" \
    -g "$NCCL_GPU_NUM" \
    -t "$NCCL_BENCH_TIMOUT" \
    -l "$THRESHOLD_MORE_THAN" \
    -d "$DRAIN_SLURM_STATE" \
    -u "$USE_INFINIBAND" \
    -n "$K8S_NAMESPACE" \
    -h "$KUBERNETES_SERVICE_HOST" \
    -p "$KUBERNETES_SERVICE_PORT" \
    -s "$SEND_JOBS_EVENTS" \
    -m "$SEND_OTEL_METRICS_GRPC" \
    -w "$SEND_OTEL_METRICS_HTTP" \
    -c "$OTEL_COLLECTOR_ENDPOINT" \
    -q "$OTEL_COLLECTOR_PATH"
