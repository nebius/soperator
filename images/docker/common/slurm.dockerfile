FROM nvidia/cuda:12.2.2-cudnn8-devel-ubuntu20.04 as slurm

ARG DEBIAN_FRONTEND=noninteractive

# TODO: Install only those dependencies that are required for building Slurm
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

# Download Slurm
ARG SLURM_VERSION=23.11.6
RUN cd /usr/src && \
    wget https://download.schedmd.com/slurm/slurm-${SLURM_VERSION}.tar.bz2 && \
    tar -xvf slurm-${SLURM_VERSION}.tar.bz2 && \
    rm -rf slurm-${SLURM_VERSION}.tar.bz2

# Install PMIx in order to build Slurm with PMIx support
# Slurm deb packages will be already compiled with PMIx support even without it, but only with v3, while we use v5
COPY docker/common/scripts/install_pmix.sh /opt/bin/
RUN chmod +x /opt/bin/install_pmix.sh && \
    /opt/bin/install_pmix.sh && \
    rm /opt/bin/install_pmix.sh

# Build deb packages for Slurm
RUN cd /usr/src/slurm-${SLURM_VERSION} && \
    mk-build-deps -i debian/control -t "apt-get -o Debug::pkgProblemResolver=yes --no-install-recommends -y" && \
    debuild -b -uc -us

################################################################
# RESULT
################################################################
# /usr/src/slurm-smd-client_23.11.6-1_amd64.deb
# /usr/src/slurm-smd-dev_23.11.6-1_amd64.deb
# /usr/src/slurm-smd-doc_23.11.6-1_all.deb
# /usr/src/slurm-smd-libnss-slurm_23.11.6-1_amd64.deb
# /usr/src/slurm-smd-libpam-slurm-adopt_23.11.6-1_amd64.deb
# /usr/src/slurm-smd-libpmi0_23.11.6-1_amd64.deb
# /usr/src/slurm-smd-libpmi2-0_23.11.6-1_amd64.deb
# /usr/src/slurm-smd-libslurm-perl_23.11.6-1_amd64.deb
# /usr/src/slurm-smd-openlava_23.11.6-1_all.deb
# /usr/src/slurm-smd-sackd_23.11.6-1_amd64.deb
# /usr/src/slurm-smd-slurmctld_23.11.6-1_amd64.deb
# /usr/src/slurm-smd-slurmd_23.11.6-1_amd64.deb
# /usr/src/slurm-smd-slurmdbd_23.11.6-1_amd64.deb
# /usr/src/slurm-smd-slurmrestd_23.11.6-1_amd64.deb
# /usr/src/slurm-smd-sview_23.11.6-1_amd64.deb
# /usr/src/slurm-smd-torque_23.11.6-1_all.deb
# /usr/src/slurm-smd_23.11.6-1_amd64.deb
################################################################
