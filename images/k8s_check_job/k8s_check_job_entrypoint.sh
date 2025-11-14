#!/bin/bash

set -euxo pipefail # Exit immediately if any command returns a non-zero error code

echo "Bind-mount DNS configuration"
mount --bind /etc/resolv.conf /mnt/jail/etc/resolv.conf

echo "Bind-mount /etc/hosts"
mount --bind /etc/hosts /mnt/jail/etc/hosts

exec "$@"
