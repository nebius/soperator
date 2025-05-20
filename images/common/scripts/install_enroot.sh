#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code

ENROOT_VERSION=3.5.0

apt-get update
apt -y install enroot=${ENROOT_VERSION}-1 enroot+caps=${ENROOT_VERSION}-1
apt-get clean
rm -rf /var/lib/apt/lists/*

# Add an extra hook that sets env vars for PyTorch
curl -fSsL -o /etc/enroot/hooks.d/50-slurm-pytorch.sh "https://raw.githubusercontent.com/NVIDIA/enroot/refs/tags/v${ENROOT_VERSION}/conf/hooks/extra/50-slurm-pytorch.sh"
chmod +x /etc/enroot/hooks.d/50-slurm-pytorch.sh

# Prepare env for running enroot
mkdir -m 777 /usr/share/enroot/enroot-data
mkdir -m 755 /run/enroot
