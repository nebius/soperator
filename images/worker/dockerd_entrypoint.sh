#!/bin/bash

# Starts dockerd (passed as arguments) only when Docker is enabled for this NodeSet.
# SOPERATOR_DOCKER_ENABLED is set by the operator from the NodeSet docker.enabled flag.
# Exits 0 otherwise, so supervisord with autorestart=unexpected doesn't respawn it.

set -euo pipefail

if [ "${SOPERATOR_DOCKER_ENABLED:-true}" != "true" ]; then
    echo "Not starting dockerd: Docker is disabled on this NodeSet because it has no image-storage disks"
    exit 0
fi

exec "$@"
