#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code

ENROOT_VERSION=4.0.1

apt-get update
apt -y install enroot=${ENROOT_VERSION}-1 enroot+caps=${ENROOT_VERSION}-1
apt-get clean
rm -rf /var/lib/apt/lists/*

# Prepare env for running enroot
mkdir -m 777 /usr/share/enroot/enroot-data
mkdir -m 755 /run/enroot
mkdir -m 755 -p /etc/enroot/enroot.conf.d
