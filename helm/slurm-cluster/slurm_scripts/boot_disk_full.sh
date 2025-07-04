#!/bin/bash

set -eox

echo "[$(date)] Check if node boot disk is full"

THRESHOLD=80
BOOT_DISK_DIR="/tmp"

# Assume that this is the boot disk under $BOOT_DISK_DIR
if ! mountpoint -q "$BOOT_DISK_DIR"; then
    echo "$BOOT_DISK_DIR is not a mountpoint" >&2
    exit 0
fi

echo "Get the usage percentage and strip the '%'"
read -r usage < <(/bin/df -P "$BOOT_DISK_DIR" | awk 'NR==2 { sub(/%/,"",$5); print $5 }' || echo '')

if [[ -z "${usage}" ]]; then
    echo "Could not determine boot disk usage" >&2
    exit 0
fi

echo "Node boot disk (the one under $BOOT_DISK_DIR) is ${usage}% full (threshold $THRESHOLD%)"

if [ "$usage" -gt "$THRESHOLD" ]; then
    # Return failure details
    echo "$usage% of space is used" >&3
    exit 1
fi

exit 0
