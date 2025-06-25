# Or `debug`
ARG MODE=release

FROM spanknccldebug-base AS builder

RUN dnf install -y cmake

COPY src/ /usr/src/spanknccldebug/
WORKDIR /usr/src/spanknccldebug/
SHELL ["/bin/bash", "-c"]

FROM builder AS build-release
CMD gcc \
        -std=gnu99 \
        -fPIC \
        -flto \
        -O3 \
        -s \
        -DNDEBUG \
        -I/usr/local/include/slurm \
        -I. \
        -L/usr/local/lib \
        -lslurm \
        -shared \
        -o build/spanknccldebug.so \
        ./*.c

FROM builder AS build-debug
CMD gcc \
        -std=gnu99 \
        -fPIC \
        -flto \
        -O0 \
        -g \
        -I/usr/local/include/slurm \
        -I. \
        -L/usr/local/lib \
        -lslurm \
        -shared \
        -o build/spanknccldebug.so \
        ./*.c

FROM build-${MODE} AS build
