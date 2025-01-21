#!/bin/bash

# Compile and install chroot SPANK plugin
gcc -fPIC -shared -o /usr/src/chroot-plugin/chroot.so /usr/src/chroot-plugin/chroot.c -I/usr/local/include/slurm -L/usr/local/lib -lslurm && \
    cp /usr/src/chroot-plugin/chroot.so /usr/lib/x86_64-linux-gnu/slurm/
