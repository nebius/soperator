#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code

echo "Link users from jail"
ln -s /mnt/jail/etc/passwd /etc/passwd
ln -s /mnt/jail/etc/group /etc/group
ln -s /mnt/jail/etc/shadow /etc/shadow
ln -s /mnt/jail/etc/gshadow /etc/gshadow
chown -h 0:42 /etc/{shadow,gshadow}

echo "Bind-mount REST JWT secret key from K8S secret"
touch /var/spool/slurmctld/jwt_hs256.key
mount --bind /mnt/rest-jwt-key/rest_jwt.key /var/spool/slurmctld/jwt_hs256.key

echo "Symlink slurm configs from K8S config map"
rm -rf /etc/slurm && ln -s /mnt/jail/etc/slurm /etc/slurm

echo "Set permissions for shared /var/spool/slurmctld"
chmod 755 /var/spool/slurmctld # It changes permissions of this shared directory in other containers as well

# TODO: Since 1.29 kubernetes supports native sidecar containers. We can remove it in feature releases
echo "Waiting until munge started"
while [ ! -S "/run/munge/munge.socket.2" ]; do sleep 2; done

# Hack with logs: multilog will write log in stdout and in log file, and rotate log file
# # s100000000 (bytes) - 100MB, n5 - 5 files

echo "Start slurmctld daemon"
exec /usr/sbin/slurmctld -D 2>&1 | tee >(multilog s100000000 n5 /var/log/slurm/multilog)
