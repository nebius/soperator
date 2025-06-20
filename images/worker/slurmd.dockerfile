# BASE_IMAGE defined here for second multistage build
ARG BASE_IMAGE=cr.eu-north1.nebius.cloud/soperator/ubuntu:jammy

# First stage: Build the gpubench application
FROM golang:1.24 AS gpubench_builder

ARG GO_LDFLAGS=""
ARG CGO_ENABLED=0
ARG GOOS=linux

WORKDIR /app

COPY images/worker/gpubench/go.mod images/worker/gpubench/go.sum ./

RUN go mod download

COPY images/worker/gpubench/main.go .

RUN GOOS=$GOOS CGO_ENABLED=$CGO_ENABLED GO_LDFLAGS=$GO_LDFLAGS \
    go build -o gpubench .

#######################################################################################################################
# Second stage: Build worker image

ARG BASE_IMAGE=cr.eu-north1.nebius.cloud/soperator/ubuntu:jammy

FROM $BASE_IMAGE AS worker_slurmd

ARG SLURM_VERSION=24.11.5
ARG OPENMPI_VERSION=4.1.7a1
ARG PYXIS_VERSION=0.21.0
# ARCH has the short form like: amd64, arm64
ARG ARCH
# ALT_ARCH has the extended form like: x86_64, aarch64
ARG ALT_ARCH

ARG DEBIAN_FRONTEND=noninteractive

# Install dependencies
RUN apt-get update && \
    apt -y install \
        wget \
        curl \
        libssl-dev \
        libpam0g-dev \
        libtool \
        libjansson-dev \
        libjson-c-dev \
        libmunge-dev \
        libhwloc-dev \
        liblz4-dev \
        flex \
        libevent-dev \
        jq \
        squashfs-tools \
        zstd \
        software-properties-common \
        iputils-ping \
        dnsutils \
        telnet \
        strace \
        vim \
        tree \
        lsof \
        pciutils \
        iproute2 \
        infiniband-diags \
        kmod \
        daemontools \
        libncurses5-dev \
        libdrm-dev \
        sudo \
        supervisor \
        openssh-server \
        rdma-core \
        ibverbs-utils \
        libpmix2 \
        libpmix-dev && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Install OpenMPI
COPY images/common/scripts/install_openmpi.sh /opt/bin/
RUN chmod +x /opt/bin/install_openmpi.sh && \
    /opt/bin/install_openmpi.sh && \
    rm /opt/bin/install_openmpi.sh

ENV LD_LIBRARY_PATH=/lib/${ALT_ARCH}-linux-gnu:/usr/lib/${ALT_ARCH}-linux-gnu:/usr/local/cuda/targets/${ALT_ARCH}-linux/lib:/usr/mpi/gcc/openmpi-${OPENMPI_VERSION}/lib
ENV PATH=$PATH:/usr/mpi/gcc/openmpi-${OPENMPI_VERSION}/bin

# Add Nebius public registry
RUN curl -fsSL https://dr.nebius.cloud/public.gpg -o /usr/share/keyrings/nebius.gpg.pub && \
    echo "deb [signed-by=/usr/share/keyrings/nebius.gpg.pub] https://dr.nebius.cloud/ stable main" > /etc/apt/sources.list.d/nebius.list

RUN apt-get update && \
    apt -y install \
      slurm-smd-client=${SLURM_VERSION}-1 \
      slurm-smd-dev=${SLURM_VERSION}-1 \
      slurm-smd-libnss-slurm=${SLURM_VERSION}-1 \
      slurm-smd=${SLURM_VERSION}-1 \
      slurm-smd-slurmd=${SLURM_VERSION}-1 && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Install slurm сhroot plugin
COPY images/common/chroot-plugin/chroot.c /usr/src/chroot-plugin/
COPY images/common/scripts/install_chroot_plugin.sh /opt/bin/
RUN chmod +x /opt/bin/install_chroot_plugin.sh && \
    ALT_ARCH=${ALT_ARCH} /opt/bin/install_chroot_plugin.sh && \
    rm /opt/bin/install_chroot_plugin.sh

# Install NCCL debug plugin
COPY images/common/spank-nccl-debug/src /usr/src/soperator/spank/nccld-debug
COPY images/common/scripts/install_nccld_debug_plugin.sh /opt/bin/
RUN chmod +x /opt/bin/install_nccld_debug_plugin.sh && \
    ALT_ARCH=${ALT_ARCH} /opt/bin/install_nccld_debug_plugin.sh && \
    rm /opt/bin/install_nccld_debug_plugin.sh && \
    mkdir -p /opt/soperator-outputs/nccl_logs

# Install parallel because it's required for enroot operation
RUN apt-get update && \
    apt -y install parallel=20210822+ds-2 && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Install enroot
COPY images/common/scripts/install_enroot.sh /opt/bin/
RUN chmod +x /opt/bin/install_enroot.sh && \
    /opt/bin/install_enroot.sh && \
    rm /opt/bin/install_enroot.sh

# Copy enroot configuration
COPY images/common/enroot/enroot.conf /etc/enroot/
RUN chown 0:0 /etc/enroot/enroot.conf && chmod 644 /etc/enroot/enroot.conf

# Install slurm pyxis plugin
RUN apt-get update && \
    apt -y install nvslurm-plugin-pyxis=${SLURM_VERSION}-${PYXIS_VERSION}-1 && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Install nvidia-container-toolkit
COPY images/common/scripts/install_container_toolkit.sh /opt/bin/
RUN chmod +x /opt/bin/install_container_toolkit.sh && \
    /opt/bin/install_container_toolkit.sh && \
    rm /opt/bin/install_container_toolkit.sh

# Copy NVIDIA Container Toolkit config
COPY images/common/nvidia-container-runtime/config.toml /etc/nvidia-container-runtime/config.toml

# Install Docker
COPY images/common/scripts/install_docker.sh /opt/bin/
RUN chmod +x /opt/bin/install_docker.sh && \
    /opt/bin/install_docker.sh && \
    rm /opt/bin/install_docker.sh

# Copy Docker daemon config
COPY images/worker/docker/daemon.json /etc/docker/daemon.json

# Copy GPU healthcheck script
COPY images/worker/scripts/gpu_healthcheck.sh /usr/bin/gpu_healthcheck.sh

# Copy script for complementing jail filesystem in runtime
COPY images/common/scripts/complement_jail.sh /opt/bin/slurm/

# Copy script for bind-mounting slurm into the jail
COPY images/common/scripts/bind_slurm_common.sh /opt/bin/slurm/

RUN chmod +x /usr/bin/gpu_healthcheck.sh && \
    chmod +x /opt/bin/slurm/complement_jail.sh && \
    chmod +x /opt/bin/slurm/bind_slurm_common.sh

# Update linker cache
RUN ldconfig

# Delete users & home because they will be linked from jail
RUN rm /etc/passwd* /etc/group* /etc/shadow* /etc/gshadow*
RUN rm -rf /home

# Delete SSH "message of the day" scripts because they aren't needed on worker nodes
RUN rm -rf /etc/update-motd.d/*

# Expose the port used for accessing slurmd
EXPOSE 6818

# Copy binary that performs GPU benchmark
COPY --from=gpubench_builder /app/gpubench /usr/bin/

# Create dir and file for multilog hack
RUN mkdir -p /var/log/slurm/multilog && \
    touch /var/log/slurm/multilog/current && \
    ln -s /var/log/slurm/multilog/current /var/log/slurm/slurmd.log

# Copy slurmd entrypoint script
COPY images/worker/slurmd_entrypoint.sh /opt/bin/slurm/

# Copy supervisord entrypoint script
COPY images/worker/supervisord_entrypoint.sh /opt/bin/slurm/

RUN chmod +x /opt/bin/slurm/slurmd_entrypoint.sh && \
    chmod +x /opt/bin/slurm/supervisord_entrypoint.sh

# Start supervisord that manages both slurmd and sshd as child processes
ENTRYPOINT ["/opt/bin/slurm/supervisord_entrypoint.sh"]
