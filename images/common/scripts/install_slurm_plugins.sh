#!/bin/bash

# Compile and install chroot SPANK plugin
gcc -fPIC -shared -o /usr/src/chroot-plugin/chroot.so /usr/src/chroot-plugin/chroot.c -I/usr/local/include/slurm -L/usr/local/lib -lslurm && \
    cp /usr/src/chroot-plugin/chroot.so /usr/lib/x86_64-linux-gnu/slurm/

# Download, compile and install pyxis SPANK plugin
cd /usr/src && \
    wget https://github.com/itechdima/pyxis/archive/refs/heads/disable-concurrent-pull-and-keep-squashfs.tar.gz && \
    tar -xzvf disable-concurrent-pull-and-keep-squashfs.tar.gz && \
    rm disable-concurrent-pull-and-keep-squashfs.tar.gz && \
    cd pyxis-disable-concurrent-pull-and-keep-squashfs && \
    make install prefix=/usr libdir=/usr/lib/x86_64-linux-gnu
