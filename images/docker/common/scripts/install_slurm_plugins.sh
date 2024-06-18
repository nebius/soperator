#!/bin/bash

# Compile and install chroot SPANK plugin
gcc -fPIC -shared -o /usr/src/chroot-plugin/chroot.so /usr/src/chroot-plugin/chroot.c -I/usr/local/include/slurm -L/usr/local/lib -lslurm && \
    cp /usr/src/chroot-plugin/chroot.so /usr/lib/x86_64-linux-gnu/slurm/

# Download, compile and install pyxis SPANK plugin
cd /usr/src && \
    wget https://github.com/NVIDIA/pyxis/archive/refs/tags/v0.19.0.tar.gz && \
    tar -xzvf v0.19.0.tar.gz && \
    rm v0.19.0.tar.gz && \
    cd pyxis-0.19.0 && \
    make install prefix=/usr libdir=/usr/lib/x86_64-linux-gnu
