#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code

echo "Bind-mount munge key from K8S secret"
mount --bind /mnt/munge-key/munge.key /etc/munge/munge.key

echo "Set permissions for shared /run/munge"
chmod 755 /run/munge # It changes permissions of this shared directory in other containers as well

echo "Start munge daemon"
exec munged -F --num-threads="$MUNGE_NUM_THREADS" --key-file="$MUNGE_KEY_FILE" --pid-file="$MUNGE_PID_FILE" -S "$MUNGE_SOCKET_FILE"
