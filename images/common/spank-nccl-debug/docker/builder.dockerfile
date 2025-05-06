FROM spanknccldebug-base AS builder

RUN dnf install -y cmake

COPY src/ /usr/src/spanknccldebug/
WORKDIR /usr/src/spanknccldebug/
SHELL ["/bin/bash", "-c"]
CMD gcc \
        -fPIC \
        -shared \
        -I/usr/local/include/slurm \
        -I. \
        -L/usr/local/lib \
        -lslurm \
        -o build/spanknccldebug.so \
        snccld.c
