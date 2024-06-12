FROM ubuntu:focal as controller_slurmctld

ARG DEBIAN_FRONTEND=noninteractive

# TODO: Install only those dependencies that are required for running slurmctld + useful utilities
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
        lsof

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
COPY --from=slurm /usr/src/slurm-smd-slurmctld_23.11.6-1_amd64.deb /tmp
COPY --from=slurm /usr/src/slurm-smd_23.11.6-1_amd64.deb /tmp/
RUN apt install -y /tmp/*.deb && rm -rf /tmp/*.deb

# Install slurm plugins
COPY docker/common/chroot-plugin/chroot.c /usr/src/chroot-plugin/
COPY docker/common/scripts/install_slurm_plugins.sh /opt/bin/
RUN chmod +x /opt/bin/install_slurm_plugins.sh && \
    /opt/bin/install_slurm_plugins.sh && \
    rm /opt/bin/install_slurm_plugins.sh

# Update linker cache
RUN ldconfig

# Delete users & home because they will be linked from jail
RUN rm /etc/passwd* /etc/group* /etc/shadow* /etc/gshadow*
RUN rm -rf /home

# Expose the port used for accessing slurmctld
EXPOSE 6817

# Copy & run the entrypoint script
COPY docker/controller/slurmctld_entrypoint.sh /opt/bin/slurm/
RUN chmod +x /opt/bin/slurm/slurmctld_entrypoint.sh
ENTRYPOINT /opt/bin/slurm/slurmctld_entrypoint.sh
