ARG BASE_IMAGE=cr.eu-north1.nebius.cloud/soperator/ubuntu:jammy

FROM $BASE_IMAGE AS controller_slurmdbd

ARG SLURM_VERSION=24.05.5

ARG DEBIAN_FRONTEND=noninteractive

# TODO: Install only those dependencies that are required for running slurmdbd + useful utilities
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
        iputils-ping \
        dnsutils \
        telnet \
        strace \
        vim \
        tree \
        lsof \
        daemontools && \
    apt clean

ARG PACKAGES_REPO_URL="https://github.com/nebius/slurm-deb-packages/releases/download"
# Download and install Slurm packages
RUN for pkg in slurm-smd-client slurm-smd-dev slurm-smd-libnss-slurm slurm-smd slurm-smd-slurmdbd; do \
        wget -q -P /tmp $PACKAGES_REPO_URL/slurm-packages-$SLURM_VERSION/${pkg}_$SLURM_VERSION-1_amd64.deb && \
        echo "${pkg}_$SLURM_VERSION-1_amd64.deb successfully downloaded" || \
        { echo "Failed to download ${pkg}_$SLURM_VERSION-1_amd64.deb"; exit 1; }; \
    done && \
    apt install -y /tmp/*.deb && \
    rm -rf /tmp/*.deb && \
    apt clean

# Update linker cache
RUN ldconfig

# Expose the port used for accessing slurmdbd
EXPOSE 6819

# Create dir and file for multilog hack
RUN mkdir -p /var/log/slurm/multilog && \
    touch /var/log/slurm/multilog/current && \
    ln -s /var/log/slurm/multilog/current /var/log/slurm/slurmdbd.log

# Copy & run the entrypoint script
COPY accounting/slurmdbd_entrypoint.sh /opt/bin/slurm/
RUN chmod +x /opt/bin/slurm/slurmdbd_entrypoint.sh
ENTRYPOINT ["/opt/bin/slurm/slurmdbd_entrypoint.sh"]
