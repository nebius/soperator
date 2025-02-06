#!/bin/bash

# Download enroot
curl -fSsL -o /tmp/enroot_3.5.0-1_amd64.deb https://github.com/NVIDIA/enroot/releases/download/v3.5.0/enroot_3.5.0-1_amd64.deb
curl -fSsL -o /tmp/enroot+caps_3.5.0-1_amd64.deb https://github.com/NVIDIA/enroot/releases/download/v3.5.0/enroot+caps_3.5.0-1_amd64.deb

# Install downloaded packages & rm them
apt install -y /tmp/*.deb
rm -rf /tmp/*.deb
apt clean

# Prepare env for running enroot
mkdir -m 777 /usr/share/enroot/enroot-data
mkdir -m 755 /run/enroot

# Set capabilities for running containers without privileges
setcap cap_sys_admin+pe /usr/bin/enroot-mksquashovlfs
setcap cap_sys_admin,cap_mknod+pe /usr/bin/enroot-aufs2ovlfs
