#!/bin/bash

set -e
set -u

# ALT_ARCH has the extended form like: x86_64, aarch64
ALT_ARCH="$(uname -m)"

ALLOC_MEM_DIAGNOSTIC_SRC_DIR=/usr/src/soperator/spank/alloc-mem-diagnostic

# Compile and install the allocated-memory diagnostic SPANK plugin.
gcc \
  -std=gnu99 \
  -fPIC \
  -flto \
  -O3 \
  -s \
  -DNDEBUG \
  -I/usr/local/include/slurm \
  -I${ALLOC_MEM_DIAGNOSTIC_SRC_DIR} \
  -L/usr/local/lib \
  -lslurm \
  -shared \
  -o /usr/lib/"${ALT_ARCH}"-linux-gnu/slurm/spank_alloc_mem_diagnostic.so \
  ${ALLOC_MEM_DIAGNOSTIC_SRC_DIR}/*.c
