ARG BASE_IMAGE=ubuntu:jammy

FROM $BASE_IMAGE AS slurmrestd

ARG SLURM_VERSION=24.05.5

ARG DEBIAN_FRONTEND=noninteractive

# TODO: Install only those dependencies that are required for running slurmrestd + useful utilities
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
        vim \
        tree \
        lsof \
        daemontools && \
    apt clean

ARG PACKAGES_REPO_URL="https://github.com/nebius/slurm-deb-packages/releases/download"
# Download and install Slurm packages
RUN for pkg in slurm-smd slurm-smd-slurmrestd; do \
        wget -q -P /tmp $PACKAGES_REPO_URL/slurm-packages-$SLURM_VERSION/${pkg}_$SLURM_VERSION-1_amd64.deb && \
        echo "${pkg}_$SLURM_VERSION-1_amd64.deb successfully downloaded" || \
        { echo "Failed to download ${pkg}_$SLURM_VERSION-1_amd64.deb"; exit 1; }; \
    done && \
    apt install -y /tmp/*.deb && \
    rm -rf /tmp/*.deb && \
    apt clean

# Expose the port used for accessing slurmrestd
EXPOSE 6820

# Copy restd conf file (overwrite AuthType)
COPY restd/slurm_rest.conf /etc/slurm/slurm_rest.conf

# Copy & run the entrypoint script
COPY restd/slurmrestd_entrypoint.sh /opt/bin/slurm/
RUN chmod +x /opt/bin/slurm/slurmrestd_entrypoint.sh
ENTRYPOINT ["/opt/bin/slurm/slurmrestd_entrypoint.sh"]
