FROM nvidia/cuda:12.2.2-cudnn8-devel-ubuntu20.04 as worker_slurmd

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
COPY docker/common/scripts/install_pmix.sh /opt/bin/
RUN chmod +x /opt/bin/install_pmix.sh && \
    /opt/bin/install_pmix.sh && \
    rm /opt/bin/install_pmix.sh

# TODO: Install only necessary packages
# Copy and install Slurm packages
COPY --from=slurm /usr/src/slurm-smd-client_23.11.6-1_amd64.deb /tmp/
COPY --from=slurm /usr/src/slurm-smd-dev_23.11.6-1_amd64.deb /tmp/
COPY --from=slurm /usr/src/slurm-smd-libnss-slurm_23.11.6-1_amd64.deb /tmp/
COPY --from=slurm /usr/src/slurm-smd-libpmi0_23.11.6-1_amd64.deb /tmp/
COPY --from=slurm /usr/src/slurm-smd-libpmi2-0_23.11.6-1_amd64.deb /tmp/
COPY --from=slurm /usr/src/slurm-smd-libslurm-perl_23.11.6-1_amd64.deb /tmp/
COPY --from=slurm /usr/src/slurm-smd-openlava_23.11.6-1_all.deb /tmp/
COPY --from=slurm /usr/src/slurm-smd-slurmd_23.11.6-1_amd64.deb /tmp/
COPY --from=slurm /usr/src/slurm-smd-sview_23.11.6-1_amd64.deb /tmp/
COPY --from=slurm /usr/src/slurm-smd-torque_23.11.6-1_all.deb /tmp/
COPY --from=slurm /usr/src/slurm-smd_23.11.6-1_amd64.deb /tmp/
RUN apt install -y /tmp/*.deb && rm -rf /tmp/*.deb

# Install slurm plugins
COPY docker/common/chroot-plugin/chroot.c /usr/src/chroot-plugin/
COPY docker/common/scripts/install_slurm_plugins.sh /opt/bin/
RUN chmod +x /opt/bin/install_slurm_plugins.sh && \
    /opt/bin/install_slurm_plugins.sh && \
    rm /opt/bin/install_slurm_plugins.sh

# Install nvidia-container-toolkit
COPY docker/common/scripts/install_container_toolkit.sh /opt/bin/
RUN chmod +x /opt/bin/install_container_toolkit.sh && \
    /opt/bin/install_container_toolkit.sh && \
    rm /opt/bin/install_container_toolkit.sh

# Install nvtop GPU monitoring utility
COPY docker/common/scripts/install_nvtop.sh /opt/bin/
RUN chmod +x /opt/bin/install_nvtop.sh && \
    /opt/bin/install_nvtop.sh && \
    rm /opt/bin/install_nvtop.sh

# Create node-local directories for enroot runtime data
RUN mkdir -p -m 777 /usr/share/enroot/enroot-data && \
    mkdir -p -m 755 /run/enroot

# Copy GPU healthcheck script
COPY docker/worker/scripts/gpu_healthcheck.sh /usr/bin/gpu_healthcheck.sh
RUN chmod +x /usr/bin/gpu_healthcheck.sh

# Copy script for complementing jail filesystem in runtime
COPY docker/common/scripts/complement_jail.sh /opt/bin/slurm/
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
COPY docker/worker/slurmd_entrypoint.sh /opt/bin/slurm/
RUN chmod +x /opt/bin/slurm/slurmd_entrypoint.sh
ENTRYPOINT /opt/bin/slurm/slurmd_entrypoint.sh
