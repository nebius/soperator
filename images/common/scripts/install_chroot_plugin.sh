#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code
set -u  # Treat unset variables as an error and exit immediately

# Check if ALT_ARCH is set and not empty
if [ -z "${ALT_ARCH:-}" ]; then
  echo "❌ ALT_ARCH is not set. Please set the ALT_ARCH environment variable (e.g., x86_64, aarch64)."
  exit 1
fi

# Compile and install chroot SPANK plugin
gcc -fPIC -shared -o /usr/src/chroot-plugin/chroot.so /usr/src/chroot-plugin/chroot.c -I/usr/local/include/slurm -L/usr/local/lib -lslurm && \
    cp /usr/src/chroot-plugin/chroot.so /usr/lib/"${ALT_ARCH}"-linux-gnu/slurm/
