#!/bin/bash

OPENMPI_VERSION=4.1.7a1-1.2310055
OPENMPI_VERSION_SHORT=4.1.7a1
UCX_VERSION=1.16.0-1.2310213
OFED_VERSION=23.10-2.1.3.1
DISTRO=$(. /etc/os-release; echo "$ID""$VERSION_ID")
cd /etc/apt/sources.list.d || exit
wget https://linux.mellanox.com/public/repo/mlnx_ofed/$OFED_VERSION/"$DISTRO"/mellanox_mlnx_ofed.list
wget -qO - https://www.mellanox.com/downloads/ofed/RPM-GPG-KEY-Mellanox | apt-key add -
apt update
apt install openmpi="$OPENMPI_VERSION" ucx="$UCX_VERSION"
apt clean

echo "export PATH=\$PATH:/usr/mpi/gcc/openmpi-${OPENMPI_VERSION_SHORT}/bin" > /etc/profile.d/path_openmpi.sh
source /etc/profile.d/path_openmpi.sh

printf "/lib/x86_64-linux-gnu\n/usr/lib/x86_64-linux-gnu\n/usr/local/cuda/targets/x86_64-linux/lib\n/usr/mpi/gcc/openmpi-%s/lib" "${OPENMPI_VERSION_SHORT}" > /etc/ld.so.conf.d/openmpi.conf
ldconfig
