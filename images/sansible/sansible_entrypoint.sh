#!/bin/bash

set -euxo pipefail # Exit immediately if any command returns a non-zero error code

echo "Bind-mount virtual filesystems"
mount -t proc /proc /mnt/jail/proc/
mount -t sysfs /sys /mnt/jail/sys/
mount --rbind /dev /mnt/jail/dev/
mount --rbind /run /mnt/jail/run/

echo "Bind-mount cgroup filesystem"
mount --rbind /sys/fs/cgroup /mnt/jail/sys/fs/cgroup

echo "Bind-mount /tmp"
mount --bind /tmp /mnt/jail/tmp/

echo "Bind-mount DNS configuration"
mount --bind /etc/resolv.conf /mnt/jail/etc/resolv.conf

echo "Bind-mount /etc/hosts"
mount --bind /etc/hosts /mnt/jail/etc/hosts

exec "$@"
