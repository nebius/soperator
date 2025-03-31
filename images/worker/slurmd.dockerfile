# BASE_IMAGE defined here for second multistage build
ARG BASE_IMAGE=cr.eu-north1.nebius.cloud/soperator/ubuntu:jammy

# First stage: Build the gpubench application
FROM golang:1.24 AS gpubench_builder

ARG GO_LDFLAGS=""
ARG CGO_ENABLED=0
ARG GOOS=linux
ARG GOARCH=amd64

WORKDIR /app

COPY worker/gpubench/go.mod worker/gpubench/go.sum ./

RUN go mod download

COPY worker/gpubench/main.go .

RUN GOOS=$GOOS GOARCH=$GOARCH CGO_ENABLED=$CGO_ENABLED GO_LDFLAGS=$GO_LDFLAGS \
    go build -o gpubench .

#######################################################################################################################
# Second stage: Build worker image

ARG BASE_IMAGE=cr.eu-north1.nebius.cloud/soperator/ubuntu:jammy

FROM $BASE_IMAGE AS worker_slurmd

ARG SLURM_VERSION=24.05.5
ARG OPENMPI_VERSION=4.1.7a1

ARG DEBIAN_FRONTEND=noninteractive

# TODO: Install only those dependencies that are required for running slurmd + useful utilities
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
    apt clean

# Install OpenMPI
COPY common/scripts/install_openmpi.sh /opt/bin/
RUN chmod +x /opt/bin/install_openmpi.sh && \
    /opt/bin/install_openmpi.sh && \
    rm /opt/bin/install_openmpi.sh

ENV LD_LIBRARY_PATH=/lib/x86_64-linux-gnu:/usr/lib/x86_64-linux-gnu:/usr/local/nvidia/lib:/usr/local/nvidia/lib64:/usr/local/cuda/targets/x86_64-linux/lib:/usr/mpi/gcc/openmpi-${OPENMPI_VERSION}/lib
ENV PATH=$PATH:/usr/mpi/gcc/openmpi-${OPENMPI_VERSION}/bin

ARG PACKAGES_REPO_URL="https://github.com/nebius/slurm-deb-packages/releases/download"
# Download and install Slurm packages
RUN for pkg in slurm-smd-client slurm-smd-dev slurm-smd-libnss-slurm slurm-smd slurm-smd-slurmd; do \
        wget -q -P /tmp $PACKAGES_REPO_URL/slurm-packages-$SLURM_VERSION/${pkg}_$SLURM_VERSION-1_amd64.deb && \
        echo "${pkg}_$SLURM_VERSION-1_amd64.deb successfully downloaded" || \
        { echo "Failed to download ${pkg}_$SLURM_VERSION-1_amd64.deb"; exit 1; }; \
    done && \
    apt install -y /tmp/*.deb && \
    rm -rf /tmp/*.deb && \
    apt clean

# Install slurm сhroot plugin
COPY common/chroot-plugin/chroot.c /usr/src/chroot-plugin/
COPY common/scripts/install_chroot_plugin.sh /opt/bin/
RUN chmod +x /opt/bin/install_chroot_plugin.sh && \
    /opt/bin/install_chroot_plugin.sh && \
    rm /opt/bin/install_chroot_plugin.sh

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
COPY common/enroot/enroot.conf /etc/enroot/
RUN chown 0:0 /etc/enroot/enroot.conf && chmod 644 /etc/enroot/enroot.conf

# Install slurm pyxis plugin
COPY common/scripts/install_pyxis_plugin.sh /opt/bin/
RUN chmod +x /opt/bin/install_pyxis_plugin.sh && \
    /opt/bin/install_pyxis_plugin.sh && \
    rm /opt/bin/install_pyxis_plugin.sh

# Install nvidia-container-toolkit
COPY common/scripts/install_container_toolkit.sh /opt/bin/
RUN chmod +x /opt/bin/install_container_toolkit.sh && \
    /opt/bin/install_container_toolkit.sh && \
    rm /opt/bin/install_container_toolkit.sh

# Copy NVIDIA Container Toolkit config
COPY common/nvidia-container-runtime/config.toml /etc/nvidia-container-runtime/config.toml

# Install Docker
COPY common/scripts/install_docker.sh /opt/bin/
RUN chmod +x /opt/bin/install_docker.sh && \
    /opt/bin/install_docker.sh && \
    rm /opt/bin/install_docker.sh

# Copy Docker daemon config
COPY worker/docker/daemon.json /etc/docker/daemon.json

# Copy GPU healthcheck script
COPY worker/scripts/gpu_healthcheck.sh /usr/bin/gpu_healthcheck.sh

# Copy script for complementing jail filesystem in runtime
COPY common/scripts/complement_jail.sh /opt/bin/slurm/

# Copy script for bind-mounting slurm into the jail
COPY common/scripts/bind_slurm_common.sh /opt/bin/slurm/

# Copy script for installing invidia driver libs into the jail
COPY common/scripts/install_nvidia_libs.sh /opt/bin/slurm/

RUN chmod +x /usr/bin/gpu_healthcheck.sh && \
    chmod +x /opt/bin/slurm/complement_jail.sh && \
    chmod +x /opt/bin/slurm/bind_slurm_common.sh && \
    chmod +x /opt/bin/slurm/install_nvidia_libs.sh

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
COPY worker/slurmd_entrypoint.sh /opt/bin/slurm/

# Copy supervisord entrypoint script
COPY worker/supervisord_entrypoint.sh /opt/bin/slurm/

RUN chmod +x /opt/bin/slurm/slurmd_entrypoint.sh && \
    chmod +x /opt/bin/slurm/supervisord_entrypoint.sh

# Start supervisord that manages both slurmd and sshd as child processes
ENTRYPOINT ["/opt/bin/slurm/supervisord_entrypoint.sh"]
