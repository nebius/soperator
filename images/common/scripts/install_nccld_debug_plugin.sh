#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code
set -u  # Treat unset variables as an error and exit immediately

# ALT_ARCH has the extended form like: x86_64, aarch64
ALT_ARCH="$(uname -m)"

SNCCLD_SRC_DIR=/usr/src/soperator/spank/nccld-debug

# Compile and install NCCL debug SPANK plugin
gcc \
  -std=gnu99 \
  -fPIC \
  -flto \
  -O3 \
  -s \
  -DNDEBUG \
  -I/usr/local/include/slurm \
  -I${SNCCLD_SRC_DIR} \
  -L/usr/local/lib \
  -lslurm \
  -shared \
  -o /usr/lib/"${ALT_ARCH}"-linux-gnu/slurm/spanknccldebug.so \
  ${SNCCLD_SRC_DIR}/*.c
