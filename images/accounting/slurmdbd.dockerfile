# syntax=docker.io/docker/dockerfile-upstream:1.20.0

# https://github.com/nebius/ml-containers/blob/main/.github/workflows/neubuntu.yml
FROM cr.eu-north1.nebius.cloud/ml-containers/neubuntu:noble-20251224121141 AS controller_slurmdbd

ARG SLURM_VERSION

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
        iputils-ping \
        dnsutils \
        telnet \
        strace \
        vim \
        tree \
        lsof \
        daemontools && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Install slurm packages
RUN apt-get update && \
    apt -y install \
      slurm-smd-client=${SLURM_VERSION}-1 \
      slurm-smd-dev=${SLURM_VERSION}-1 \
      slurm-smd-libnss-slurm=${SLURM_VERSION}-1 \
      slurm-smd=${SLURM_VERSION}-1 \
      slurm-smd-slurmdbd=${SLURM_VERSION}-1 && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Create single folder with slurm plugins for all architectures
RUN mkdir -p /usr/lib/slurm && \
    for dir in /usr/lib/*-linux-gnu/slurm; do \
      [ -d "$dir" ] && ln -sf $dir/* /usr/lib/slurm/ 2>/dev/null || true; \
    done
# Update linker cache
RUN ldconfig

# Expose the port used for accessing slurmdbd
EXPOSE 6819

# Create dir and file for multilog hack
RUN mkdir -p /var/log/slurm/multilog && \
    touch /var/log/slurm/multilog/current && \
    ln -s /var/log/slurm/multilog/current /var/log/slurm/slurmdbd.log

# Copy & run the entrypoint script
COPY images/accounting/slurmdbd_entrypoint.sh /opt/bin/slurm/
RUN chmod +x /opt/bin/slurm/slurmdbd_entrypoint.sh
ENTRYPOINT ["/opt/bin/slurm/slurmdbd_entrypoint.sh"]
