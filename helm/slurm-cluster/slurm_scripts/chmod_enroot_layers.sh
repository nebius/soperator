#!/bin/bash

set -euxo pipefail

echo "[$(date)] Make image layers cached by enroot readable and writable for anyone"

IMAGE_STORAGE_VOLUME="/mnt/jail/mnt/image-storage"

if ! mountpoint "$IMAGE_STORAGE_VOLUME"; then
    echo "There is no separate volume for container images"
    exit 0
fi

ENROOT_LAYERS_DIR="$IMAGE_STORAGE_VOLUME/enroot/cache"

echo "Add read and write permissions for all files inside $ENROOT_LAYERS_DIR"
mkdir -p "$ENROOT_LAYERS_DIR" || true
chmod -R a+rw "$ENROOT_LAYERS_DIR" || true

exit 0
