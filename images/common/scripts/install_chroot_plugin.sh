#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code
set -u  # Treat unset variables as an error and exit immediately

# ALT_ARCH has the extended form like: x86_64, aarch64
ALT_ARCH="$(uname -m)"

# Compile and install chroot SPANK plugin
gcc -fPIC -shared -o /usr/src/chroot-plugin/chroot.so /usr/src/chroot-plugin/chroot.c -I/usr/local/include/slurm -L/usr/local/lib -lslurm && \
    cp /usr/src/chroot-plugin/chroot.so /usr/lib/"${ALT_ARCH}"-linux-gnu/slurm/
