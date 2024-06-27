FROM nvidia/cuda:12.2.2-cudnn8-devel-ubuntu20.04 as nccl

ARG DEBIAN_FRONTEND=noninteractive

# TODO: Install only those dependencies that are required for building NCCL
# Install dependencies
RUN apt-get update && \
    apt -y install \
        git  \
        build-essential \
        devscripts \
        debhelper \
        fakeroot \
        wget \
        equivs \
        autoconf \
        pkg-config \
        libssl-dev \
        libpam0g-dev \
        libtool \
        libjansson-dev \
        libjson-c-dev \
        munge \
        libmunge-dev \
        libjwt0 \
        libjwt-dev \
        libhwloc-dev \
        liblz4-dev \
        flex \
        libevent-dev \
        jq \
        squashfs-tools \
        zstd \
        zlibc \
        zlib1g-dev

RUN cd /usr/src && \
    git clone https://github.com/NVIDIA/nccl.git && \
    cd nccl && \
    make -j pkg.debian.build

################################################################
# RESULT
################################################################
# /usr/src/nccl/build/pkg/deb/libnccl-dev_2.22.3-1+cuda12.2_amd64.deb
# /usr/src/nccl/build/pkg/deb/libnccl2_2.22.3-1+cuda12.2_amd64.deb
################################################################
