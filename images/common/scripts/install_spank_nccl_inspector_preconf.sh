#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code

apt-get update
apt-get -y install --reinstall spank-nccl-inspector-preconf
apt-get clean
rm -rf /var/lib/apt/lists/*
