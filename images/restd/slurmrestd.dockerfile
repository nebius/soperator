ARG BASE_IMAGE=ubuntu:jammy

FROM $BASE_IMAGE AS slurmrestd

ARG SLURM_VERSION=24.05.2
ARG CUDA_VERSION=12.2.2

ARG DEBIAN_FRONTEND=noninteractive

# TODO: Install only those dependencies that are required for running slurmrestd + useful utilities
# Install dependencies
RUN apt-get update && \
    apt -y install \
        wget \
        curl \
        git \
        build-essential \
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
        vim \
        tree \
        lsof \
        daemontools


# Download and install Slurm packages
RUN for pkg in slurm-smd slurm-smd-slurmrestd; do \
        wget -q -P /tmp https://github.com/nebius/slurm-deb-packages/releases/download/$CUDA_VERSION-$(grep 'VERSION_CODENAME' /etc/os-release | cut -d= -f2)-slurm$SLURM_VERSION/${pkg}_$SLURM_VERSION-1_amd64.deb && \
        echo "${pkg}_$SLURM_VERSION-1_amd64.deb successfully downloaded" || \
        { echo "Failed to download ${pkg}_$SLURM_VERSION-1_amd64.deb"; exit 1; }; \
    done

RUN apt install -y /tmp/*.deb && rm -rf /tmp/*.deb

# Expose the port used for accessing slurmrestd
EXPOSE 6820

# Copy restd conf file (owerwrite AuthType)
COPY restd/slurm_rest.conf /etc/slurm/slurm_rest.conf

# Copy & run the entrypoint script
COPY restd/slurmrestd_entrypoint.sh /opt/bin/slurm/
RUN chmod +x /opt/bin/slurm/slurmrestd_entrypoint.sh
ENTRYPOINT ["/opt/bin/slurm/slurmrestd_entrypoint.sh"]
