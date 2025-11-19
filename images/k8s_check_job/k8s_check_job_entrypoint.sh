#!/bin/bash

set -euxo pipefail # Exit immediately if any command returns a non-zero error code

echo "Bind-mount virtual filesystems"
mount -t proc /proc proc/
mount -t sysfs /sys sys/
mount --rbind /dev dev/
mount --rbind /run run/

echo "Bind-mount cgroup filesystem"
mount --rbind /sys/fs/cgroup sys/fs/cgroup

echo "Bind-mount /tmp"
mount --bind /tmp tmp/

echo "Bind-mount DNS configuration"
mount --bind /etc/resolv.conf /mnt/jail/etc/resolv.conf

echo "Bind-mount /etc/hosts"
mount --bind /etc/hosts /mnt/jail/etc/hosts

exec "$@"
