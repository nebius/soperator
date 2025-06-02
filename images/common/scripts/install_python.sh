#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code

# Install python
add-apt-repository ppa:deadsnakes/ppa -y
apt-get update
apt -y install \
    python3.10 \
    python3.10-dev \
    python3.10-venv \
    python3.10-dbg
apt-get clean
rm -rf /var/lib/apt/lists/*

# Install pip
curl -sS https://bootstrap.pypa.io/get-pip.py | python3.10

# Make python3.10 the default python
ln -s -f /usr/bin/python3.10 /usr/bin/python && ln -s -f /usr/bin/python3.10 /usr/bin/python3
