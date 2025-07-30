#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code

# 1.17.4 latest working version
# since 1.17.7 there was commit that breaks CUDA
NVIDIA_TOOLKIT_VERSION=1.17.8-1

# Install nvidia-container-toolkit for propagating NVIDIA drivers to containers
curl -fsSL https://nvidia.github.io/libnvidia-container/gpgkey | sudo gpg --dearmor -o /usr/share/keyrings/nvidia-container-toolkit-keyring.gpg \
  && curl -s -L https://nvidia.github.io/libnvidia-container/stable/deb/nvidia-container-toolkit.list | \
    sed 's#deb https://#deb [signed-by=/usr/share/keyrings/nvidia-container-toolkit-keyring.gpg] https://#g' | \
    sudo tee /etc/apt/sources.list.d/nvidia-container-toolkit.list

apt-get update
apt-get install -y nvidia-container-toolkit=${NVIDIA_TOOLKIT_VERSION} \
    nvidia-container-toolkit-base=${NVIDIA_TOOLKIT_VERSION} \
    libnvidia-container-tools=${NVIDIA_TOOLKIT_VERSION} \
    libnvidia-container1=${NVIDIA_TOOLKIT_VERSION} \

apt-get clean
rm -rf /var/lib/apt/lists/*
