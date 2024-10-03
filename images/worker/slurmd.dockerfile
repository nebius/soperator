ARG BASE_IMAGE=ghcr.io/asteny/cuda_base:12.2.2

FROM $BASE_IMAGE AS worker_slurmd

ARG SLURM_VERSION=24.05.2
ARG CUDA_VERSION=12.2.2

ARG DEBIAN_FRONTEND=noninteractive

# TODO: Install only those dependencies that are required for running slurmd + useful utilities
# Install dependencies
RUN apt-get update && \
    apt -y install \
        wget \
        curl \
        git \
        build-essential \
        bc \
        python3  \
        autoconf \
        pkg-config \
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
        libdrm-dev

# Install PMIx
COPY common/scripts/install_pmix.sh /opt/bin/
RUN chmod +x /opt/bin/install_pmix.sh && \
    /opt/bin/install_pmix.sh && \
    rm /opt/bin/install_pmix.sh

# TODO: Install only necessary packages
# Download and install Slurm packages
RUN wget -q -P /tmp https://github.com/nebius/slurm-deb-packages/releases/download/$CUDA_VERSION-$(grep 'VERSION_CODENAME' /etc/os-release | cut -d= -f2)-slurm$SLURM_VERSION/slurm-smd-torque_$SLURM_VERSION-1_all.deb && \
    echo "slurm-smd-torque_$SLURM_VERSION-1_amd64.deb successfully downloaded" || \
    { echo "Failed to download slurm-smd-torque_$SLURM_VERSION-1_amd64.deb"; exit 1; } && \
    for pkg in slurm-smd-client slurm-smd-dev slurm-smd-libnss-slurm slurm-smd-libpmi0 slurm-smd-libpmi2-0 slurm-smd-libslurm-perl slurm-smd-slurmd slurm-smd-sview slurm-smd; do \
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

# Create node-local directories for enroot runtime data
RUN mkdir -p -m 777 /usr/share/enroot/enroot-data && \
    mkdir -p -m 755 /run/enroot

# Copy GPU healthcheck script
COPY worker/scripts/gpu_healthcheck.sh /usr/bin/gpu_healthcheck.sh
RUN chmod +x /usr/bin/gpu_healthcheck.sh

# Copy script for complementing jail filesystem in runtime
COPY common/scripts/complement_jail.sh /opt/bin/slurm/
RUN chmod +x /opt/bin/slurm/complement_jail.sh

# Update linker cache
RUN ldconfig

# Delete users & home because they will be linked from jail
RUN rm /etc/passwd* /etc/group* /etc/shadow* /etc/gshadow*
RUN rm -rf /home

# Expose the port used for accessing slurmd
EXPOSE 6818

# Create dir and file for multilog hack
RUN mkdir -p /var/log/slurm/multilog && \
    touch /var/log/slurm/multilog/current && \
    ln -s /var/log/slurm/multilog/current /var/log/slurm/slurmd.log

# Copy & run the entrypoint script
COPY worker/slurmd_entrypoint.sh /opt/bin/slurm/
RUN chmod +x /opt/bin/slurm/slurmd_entrypoint.sh
ENTRYPOINT ["/opt/bin/slurm/slurmd_entrypoint.sh"]
