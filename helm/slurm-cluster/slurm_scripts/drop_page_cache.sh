#!/bin/bash

set -euxo pipefail

echo "[$(date)] Drop page cache"

sync || true
echo 3 >/proc/sys/vm/drop_caches || true

exit 0
