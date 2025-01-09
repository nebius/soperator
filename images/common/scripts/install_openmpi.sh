#!/bin/bash

OPENMPI_VERSION=4.1.7a1-1.2310055
OFED_VERSION=23.10-2.1.3.1
DISTRO=$(. /etc/os-release; echo "$ID""$VERSION_ID")
cd /etc/apt/sources.list.d || exit
wget https://linux.mellanox.com/public/repo/mlnx_ofed/$OFED_VERSION/"$DISTRO"/mellanox_mlnx_ofed.list
wget -qO - https://www.mellanox.com/downloads/ofed/RPM-GPG-KEY-Mellanox | apt-key add -
apt update
apt install openmpi="$OPENMPI_VERSION"
