#!/bin/bash

set -euo pipefail

SOCKET_PATH=/var/run/soperator-docker.sock

mkdir -p "$(dirname "${SOCKET_PATH}")" /run/nginx
rm -f "${SOCKET_PATH}"

# Keep the unix socket world-accessible like the previous proxy.
umask 000

exec /usr/sbin/nginx -c /etc/nginx/soperator-docker-proxy.conf -g 'daemon off;'
