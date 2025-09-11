#!/bin/bash

set -euxo pipefail

echo "[$(date)] Drop /dev/shm (POSIX shared memory)"

SHM_DIR="/mnt/jail/dev/shm"
rm -rf -- $SHM_DIR/..?* $SHM_DIR/.[!.]* $SHM_DIR/* || true

exit 0
