#!/bin/bash

# Download & install parallel
curl -fSsL -o /tmp/parallel_20240622_all.deb https://download.opensuse.org/repositories/home:/tange/xUbuntu_22.04/all/parallel_20240622_all.deb
apt install -y /tmp/*.deb && rm -rf /tmp/*.deb
