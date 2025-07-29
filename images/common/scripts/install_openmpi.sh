#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code

OPENMPI_VERSION=4.1.7a1-1.2404066
OPENMPI_VERSION_SHORT=4.1.7a1
OFED_VERSION=24.04-0.7.0.0

UCX_VERSION=1.17.0-1.2404066

DISTRO=$(. /etc/os-release; echo "${ID}${VERSION_ID}")
ALT_ARCH="$(uname -m)"

cd /etc/apt/sources.list.d || exit
wget https://linux.mellanox.com/public/repo/mlnx_ofed/$OFED_VERSION/"$DISTRO"/mellanox_mlnx_ofed.list
wget -qO - https://www.mellanox.com/downloads/ofed/RPM-GPG-KEY-Mellanox | apt-key add -
apt update
apt install openmpi="$OPENMPI_VERSION" ucx="$UCX_VERSION"
apt clean
rm -rf /var/lib/apt/lists/*

echo "export PATH=\$PATH:/usr/mpi/gcc/openmpi-${OPENMPI_VERSION_SHORT}/bin" > /etc/profile.d/path_openmpi.sh
source /etc/profile.d/path_openmpi.sh

printf "/lib/${ALT_ARCH}-linux-gnu\n/usr/lib/${ALT_ARCH}-linux-gnu\n/usr/local/cuda/targets/${ALT_ARCH}-linux/lib\n/usr/mpi/gcc/openmpi-%s/lib" "${OPENMPI_VERSION_SHORT}" > /etc/ld.so.conf.d/openmpi.conf
ldconfig
