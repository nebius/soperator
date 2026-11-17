#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code

ENROOT_VERSION=4.2.1

apt-get update
apt -y install \
    enroot=${ENROOT_VERSION}-1 \
    enroot+caps=${ENROOT_VERSION}-1 \
    patch
apt-get clean
rm -rf /var/lib/apt/lists/*

# Enable hook that sets env vars for PyTorch
cp /usr/share/enroot/hooks.d/50-slurm-pytorch.sh /etc/enroot/hooks.d/50-slurm-pytorch.sh

# Let containers request the ngx driver capability (libnvidia-ngx), which the stock hook rejects.
# patch fails the build when a new Enroot version changes the patched lines or their context.
patch --fuzz=0 --forward --no-backup-if-mismatch \
    /etc/enroot/hooks.d/98-nvidia.sh /opt/bin/enroot_98-nvidia_ngx.patch

# Prepare env for running enroot
mkdir -m 777 /usr/share/enroot/enroot-data
mkdir -m 755 /run/enroot
mkdir -m 755 -p /etc/enroot/enroot.conf.d
