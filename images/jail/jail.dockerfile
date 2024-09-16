# BASE_IMAGE defined here for second multistage build
ARG BASE_IMAGE=nvidia/cuda:12.2.2-cudnn8-devel-ubuntu22.04

# First stage: Build the gpubench application
FROM golang:1.22 AS gpubench_builder

ARG GO_LDFLAGS=""
ARG CGO_ENABLED=0
ARG GOOS=linux
ARG GOARCH=amd64

WORKDIR /app

COPY jail/gpubench/go.mod jail/gpubench/go.sum ./

RUN go mod download

COPY jail/gpubench/main.go .

RUN GOOS=$GOOS GOARCH=$GOARCH CGO_ENABLED=$CGO_ENABLED GO_LDFLAGS=$GO_LDFLAGS \
    go build -o gpubench .

#######################################################################################################################
# Second stage: Build jail image

ARG BASE_IMAGE=nvidia/cuda:12.2.2-cudnn8-devel-ubuntu22.04

FROM $BASE_IMAGE AS jail

ARG SLURM_VERSION=24.05.2
ARG CUDA_VERSION=12.2.2

ARG DEBIAN_FRONTEND=noninteractive

# TODO: Install only those dependencies that are required for Slurm client + useful utilities
# Install dependencies
RUN apt update && \
    apt install -y \
        autoconf \
        bc \
        build-essential \
        curl \
        flex \
        gettext-base \
        git \
        gnupg \
        jq \
        less \
        libevent-dev \
        libhwloc-dev \
        libjansson-dev \
        libjson-c-dev \
        liblz4-dev \
        libmunge-dev \
        libopenmpi-dev \
        libpam0g-dev \
        libssl-dev \
        libtool \
        lsof \
        pkg-config \
        software-properties-common \
        squashfs-tools \
        iputils-ping \
        dnsutils \
        telnet \
        strace \
        sudo \
        tree \
        vim \
        wget \
        zstd \
        pciutils \
        iproute2 \
        infiniband-diags \
        libncurses5-dev \
        libdrm-dev \
        zip \
        unzip \
        rsync \
        aws-cli

# Install python
COPY common/scripts/install_python.sh /opt/bin/
RUN chmod +x /opt/bin/install_python.sh && \
    /opt/bin/install_python.sh && \
    rm /opt/bin/install_python.sh

# Install mpi4py
RUN pip install -U pip wheel build && pip install mpi4py

# Install parallel because it's required for enroot operation
COPY common/scripts/install_parallel.sh /opt/bin/
RUN chmod +x /opt/bin/install_parallel.sh && \
    /opt/bin/install_parallel.sh && \
    rm /opt/bin/install_parallel.sh

# Install enroot
COPY common/scripts/install_enroot.sh /opt/bin/
RUN chmod +x /opt/bin/install_enroot.sh && \
    /opt/bin/install_enroot.sh && \
    rm /opt/bin/install_enroot.sh

# Copy enroot configuration
COPY jail/enroot-conf/enroot.conf /etc/enroot/
RUN chown 0:0 /etc/enroot/enroot.conf && chmod 644 /etc/enroot/enroot.conf

# Create directory for enroot runtime data that will be mounted from the host
RUN mkdir -p -m 777 /usr/share/enroot/enroot-data

# Install PMIx
COPY common/scripts/install_pmix.sh /opt/bin/
RUN chmod +x /opt/bin/install_pmix.sh && \
    /opt/bin/install_pmix.sh && \
    rm /opt/bin/install_pmix.sh

# TODO: Install only necessary packages
# Download and install Slurm packages
RUN for pkg in slurm-smd-client slurm-smd-dev slurm-smd-libnss-slurm slurm-smd-libpmi0 slurm-smd-libpmi2-0 slurm-smd-libslurm-perl slurm-smd; do \
        wget -q -P /tmp https://github.com/nebius/slurm-deb-packages/releases/download/$CUDA_VERSION-$(grep 'VERSION_CODENAME' /etc/os-release | cut -d= -f2)-slurm$SLURM_VERSION/${pkg}_$SLURM_VERSION-1_amd64.deb && \
        echo "${pkg}_$SLURM_VERSION-1_amd64.deb successfully downloaded" || \
        { echo "Failed to download ${pkg}_$SLURM_VERSION-1_amd64.deb"; exit 1; }; \
    done

RUN apt install -y /tmp/*.deb && rm -rf /tmp/*.deb

# Install slurm plugins
COPY common/chroot-plugin/chroot.c /usr/src/chroot-plugin/
COPY common/scripts/install_slurm_plugins.sh /opt/bin/
RUN chmod +x /opt/bin/install_slurm_plugins.sh && \
    /opt/bin/install_slurm_plugins.sh && \
    rm /opt/bin/install_slurm_plugins.sh

# Create directory for bind-mounting it from the host. It's needed for sbatch to work
RUN mkdir -m 755 -p /var/spool/slurmd

# Install nvidia-container-toolkit
COPY common/scripts/install_container_toolkit.sh /opt/bin/
RUN chmod +x /opt/bin/install_container_toolkit.sh && \
    /opt/bin/install_container_toolkit.sh && \
    rm /opt/bin/install_container_toolkit.sh

# Install nvtop GPU monitoring utility
COPY common/scripts/install_nvtop.sh /opt/bin/
RUN chmod +x /opt/bin/install_nvtop.sh && \
    /opt/bin/install_nvtop.sh && \
    rm /opt/bin/install_nvtop.sh

# Download and install NCCL packages
RUN wget -P /tmp https://github.com/nebius/slurm-deb-packages/releases/download/$CUDA_VERSION-$(grep 'VERSION_CODENAME' /etc/os-release | cut -d= -f2)-slurm$SLURM_VERSION/libnccl2_2.22.3-1+cuda12.2_amd64.deb && \
    wget -P /tmp https://github.com/nebius/slurm-deb-packages/releases/download/$CUDA_VERSION-$(grep 'VERSION_CODENAME' /etc/os-release | cut -d= -f2)-slurm$SLURM_VERSION/libnccl-dev_2.22.3-1+cuda12.2_amd64.deb && \
    dpkg -i /tmp/libnccl2_2.22.3-1+cuda12.2_amd64.deb && \
    dpkg -i /tmp/libnccl-dev_2.22.3-1+cuda12.2_amd64.deb && \
    rm -rf /tmp/*.deb

# Download NCCL tests executables
RUN wget -P /tmp https://github.com/nebius/slurm-deb-packages/releases/download/$CUDA_VERSION-$(grep 'VERSION_CODENAME' /etc/os-release | cut -d= -f2)-slurm$SLURM_VERSION/nccl-tests-perf.tar.gz && \
    tar -xvzf /tmp/nccl-tests-perf.tar.gz -C /usr/bin && \
    rm -rf /tmp/nccl-tests-perf.tar.gz

# Copy binary that performs GPU benchmark
COPY --from=gpubench_builder /app/gpubench /usr/bin/

# Create directory for pivoting host's root
RUN mkdir -m 555 /mnt/host

# Copy initial users
RUN rm /etc/passwd* /etc/group* /etc/shadow* /etc/gshadow*
COPY jail/init-users/* /etc/
RUN chmod 644 /etc/passwd /etc/group && chown 0:0 /etc/passwd /etc/group && \
    chmod 640 /etc/shadow /etc/gshadow && chown 0:42 /etc/shadow /etc/gshadow

# Adjust the default $HOME directory content
RUN cd /etc/skel && \
    mkdir -m 755 .slurm && \
    touch .slurm/defaults && \
    chmod 644 .slurm/defaults && \
    cp -r /etc/skel/.slurm /root/

# Copy createuser utility script
COPY jail/scripts/createuser.sh /usr/bin/createuser
RUN chmod +x /usr/bin/createuser

# Update linker cache
RUN ldconfig
