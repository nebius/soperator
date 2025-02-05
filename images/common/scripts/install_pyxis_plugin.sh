#!/bin/bash

PYXIS_VERSION=0.21.0

wget -q -P /tmp https://github.com/nebius/slurm-deb-packages/releases/download/12.4.1-jammy-slurm24.05.5/nvslurm-plugin-pyxis_"$PYXIS_VERSION"-1_amd64.deb
dpkg -i /tmp/nvslurm-plugin-pyxis_"$PYXIS_VERSION"-1_amd64.deb
rm -rf /tmp/nvslurm-plugin-pyxis_"$PYXIS_VERSION"-1_amd64.deb
