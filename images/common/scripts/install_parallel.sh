#!/bin/bash

# Download & install parallel
curl -fSsL -o /tmp/parallel_20240222+ds-2_all.deb http://ftp.nl.debian.org/debian/pool/main/p/parallel/parallel_20240222+ds-2_all.deb
apt install -y /tmp/parallel_20240222+ds-2_all.deb && rm -rf /tmp/parallel_20240222+ds-2_all.deb
