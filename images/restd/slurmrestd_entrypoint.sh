#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code

echo "Symlink slurm configs from K8S config map"
rm -rf /etc/slurm && ln -s /mnt/slurm-configs /etc/slurm

chown www-data:www-data /usr/sbin/slurmrestd && chmod 500 /usr/sbin/slurmrestd

echo "Start slurmrestd daemon"
exec /usr/sbin/slurmrestd -f /etc/slurm/slurm_rest.conf -u www-data -g www-data -a rest_auth/jwt -vvvvvv :6820
