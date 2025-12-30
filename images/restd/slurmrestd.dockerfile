# syntax=docker.io/docker/dockerfile-upstream:1.20.0

# https://github.com/nebius/ml-containers/blob/main/.github/workflows/neubuntu.yml
FROM cr.eu-north1.nebius.cloud/ml-containers/neubuntu:noble-20251224121141 AS slurmrestd

ARG SLURM_VERSION

ARG DEBIAN_FRONTEND=noninteractive

# Install dependencies
RUN apt-get update && \
    apt -y install \
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
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Install slurm packages
RUN apt-get update && \
    apt -y install \
      slurm-smd-slurmrestd=${SLURM_VERSION}-1 \
      slurm-smd=${SLURM_VERSION}-1 && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Create single folder with slurm plugins for all architectures
RUN mkdir -p /usr/lib/slurm && \
    for dir in /usr/lib/*-linux-gnu/slurm; do \
      [ -d "$dir" ] && ln -sf $dir/* /usr/lib/slurm/ 2>/dev/null || true; \
    done

# Update linker cache
RUN ldconfig

# Expose the port used for accessing slurmrestd
EXPOSE 6820

# Copy & run the entrypoint script
COPY images/restd/slurmrestd_entrypoint.sh /opt/bin/slurm/
RUN chmod +x /opt/bin/slurm/slurmrestd_entrypoint.sh
ENTRYPOINT ["/opt/bin/slurm/slurmrestd_entrypoint.sh"]
