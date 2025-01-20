#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code

echo "Bind-mount REST JWT key from K8S secret"
touch /var/spool/slurmdbd/jwt_hs256.key
mount --bind /mnt/rest-jwt-key/rest_jwt.key /var/spool/slurmdbd/jwt_hs256.key

echo "Bind-mount slurm configs from K8S config map"
for file in /mnt/slurm-configs/*; do
    filename=$(basename "$file")
    touch "/etc/slurm/$filename" && mount --bind "$file" "/etc/slurm/$filename"
done

for file in /mnt/slurm-secrets/*; do
    filename=$(basename "$file")
    touch "/etc/slurm/$filename" && mount --bind "$file" "/etc/slurm/$filename"
done

echo "Set permissions for shared /var/spool/slurmdbd"
chmod 755 /var/spool/slurmdbd # It changes permissions of this shared directory in other containers as well

# TODO: Since 1.29 kubernetes supports native sidecar containers. We can remove it in feature releases
echo "Waiting until munge started"
while [ ! -S "/run/munge/munge.socket.2" ]; do sleep 2; done

# Hack with logs: multilog will write log in stdout and in log file, and rotate log file
# # s100000000 (bytes) - 100MB, n5 - 5 files

echo "Start slurmdbd daemon"
exec /usr/sbin/slurmdbd -D 2>&1 | tee >(multilog s100000000 n5 /var/log/slurm/multilog)
