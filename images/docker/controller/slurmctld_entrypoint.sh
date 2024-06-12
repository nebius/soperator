#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code

echo "Link users from jail"
ln -s /mnt/jail/etc/passwd /etc/passwd
ln -s /mnt/jail/etc/group /etc/group
ln -s /mnt/jail/etc/shadow /etc/shadow
ln -s /mnt/jail/etc/gshadow /etc/gshadow
chown -h 0:42 /etc/{shadow,gshadow}

echo "Bind-mount slurm configs from K8S config map"
for file in /mnt/slurm-configs/*; do
    filename=$(basename "$file")
    touch "/etc/slurm/$filename" && mount --bind "$file" "/etc/slurm/$filename"
done

echo "Set permissions for shared /var/spool/slurmctld"
chmod 755 /var/spool/slurmctld # It changes permissions of this shared directory in other containers as well

echo "Waiting until munge started"
while [ ! -S "/run/munge/munge.socket.2" ]; do sleep 2; done

echo "Start slurmctld daemon"
/usr/sbin/slurmctld -D
