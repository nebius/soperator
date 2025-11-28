#!/bin/bash

set -euxo pipefail

echo "[$(date)] Cleanup scratch data"

SCRATCH_DIR="scratch"
HOST_FS_PATH="/mnt/jail/$SCRATCH_DIR"

rm -rf -- "${HOST_FS_PATH:?}"/..?* "${HOST_FS_PATH:?}"/.[!.]* "${HOST_FS_PATH:?}"/* || true

exit 0
