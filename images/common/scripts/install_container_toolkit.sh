#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code

NVIDIA_TOOLKIT_VERSION=1.17.8-1

# Install nvidia-container-toolkit for propagating NVIDIA drivers to containers
apt-get update
apt-get install -y \
    nvidia-container-toolkit=${NVIDIA_TOOLKIT_VERSION} \
    nvidia-container-toolkit-base=${NVIDIA_TOOLKIT_VERSION} \
    libnvidia-container-tools=${NVIDIA_TOOLKIT_VERSION} \
    libnvidia-container1=${NVIDIA_TOOLKIT_VERSION}

apt-get clean
rm -rf /var/lib/apt/lists/*
