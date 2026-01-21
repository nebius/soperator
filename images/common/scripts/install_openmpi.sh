#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code

OPENMPI_VERSION=4.1.7a1-1.2404066
OPENMPI_VERSION_SHORT=4.1.7a1
OFED_VERSION=24.04-0.7.0.0

UCX_VERSION=1.17.0-1.2404066

ALT_ARCH="$(uname -m)"

apt update
apt install openmpi="$OPENMPI_VERSION" ucx="$UCX_VERSION"
apt clean
rm -rf /var/lib/apt/lists/*

echo "export PATH=\$PATH:/usr/mpi/gcc/openmpi-${OPENMPI_VERSION_SHORT}/bin" > /etc/profile.d/path_openmpi.sh
source /etc/profile.d/path_openmpi.sh

printf "/lib/${ALT_ARCH}-linux-gnu\n/usr/lib/${ALT_ARCH}-linux-gnu\n/usr/local/cuda/targets/${ALT_ARCH}-linux/lib\n/usr/mpi/gcc/openmpi-%s/lib" "${OPENMPI_VERSION_SHORT}" > /etc/ld.so.conf.d/openmpi.conf
ldconfig
