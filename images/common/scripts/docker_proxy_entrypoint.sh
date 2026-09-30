#!/bin/bash

set -euo pipefail

if [ "${SOPERATOR_DOCKER_ENABLED:-false}" != "true" ]; then
    echo "Not starting Docker proxy: Docker is disabled"
    exit 0
fi

if [ "$#" -lt 1 ]; then
    echo "Docker proxy mode is required" >&2
    exit 1
fi

mode="$1"
shift

case "${mode}" in
    login|worker) ;;
    *)
        echo "Unsupported Docker proxy mode: ${mode}" >&2
        exit 1
        ;;
esac

exec /usr/bin/soperator-docker-proxy --mode="${mode}" "$@"
