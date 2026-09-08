#!/bin/bash

set -euo pipefail

if [ "${SOPERATOR_DOCKER_ENABLED:-false}" != "true" ]; then
    echo "Not starting dockerd: login Docker is disabled"
    exit 0
fi

cgroup_base_file=/run/soperator-docker-cgroup-base
if [ ! -s "${cgroup_base_file}" ]; then
    echo "Login Docker: cgroup base is unavailable" >&2
    exit 1
fi

cgroup_base="$(cat "${cgroup_base_file}")"
exec /usr/bin/dockerd --cgroup-parent="${cgroup_base%/}/docker-unattributed"
