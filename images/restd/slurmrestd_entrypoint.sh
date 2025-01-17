#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code
set -x # Print actual command before executing it

echo "Bind-mount slurm configs from K8S config map"
for file in /mnt/slurm-configs/*; do
    filename=$(basename "$file")
    touch "/etc/slurm/$filename" && mount --bind "$file" "/etc/slurm/$filename"
done

chown www-data:www-data /usr/sbin/slurmrestd && chmod 500 /usr/sbin/slurmrestd

echo "Start slurmrestd daemon"
exec /usr/sbin/slurmrestd -f /etc/slurm/slurm_rest.conf -u www-data -g www-data -a rest_auth/jwt -vvvvvv :6820
