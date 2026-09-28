#!/bin/bash

set -xe # Exit immediately if any command returns a non-zero error code

OPENMPI_VERSION=4.1.9a1-1.20250722.92f9fca4eb.2507097
OPENMPI_VERSION_SHORT=4.1.9a1

UCX_VERSION=1.19.0-1.20250722.13ae265cb.2507097

ALT_ARCH="$(uname -m)"

apt update
apt install -y openmpi="$OPENMPI_VERSION" ucx="$UCX_VERSION"
apt clean
rm -rf /var/lib/apt/lists/*

echo "export PATH=\$PATH:/usr/mpi/gcc/openmpi-${OPENMPI_VERSION_SHORT}/bin" > /etc/profile.d/path_openmpi.sh
source /etc/profile.d/path_openmpi.sh

printf "/lib/${ALT_ARCH}-linux-gnu\n/usr/lib/${ALT_ARCH}-linux-gnu\n/usr/local/cuda/targets/${ALT_ARCH}-linux/lib\n/usr/mpi/gcc/openmpi-%s/lib" "${OPENMPI_VERSION_SHORT}" > /etc/ld.so.conf.d/openmpi.conf
ldconfig
