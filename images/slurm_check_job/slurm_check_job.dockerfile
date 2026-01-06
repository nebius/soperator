# syntax=docker.io/docker/dockerfile-upstream:1.20.0

# https://github.com/nebius/ml-containers/pull/39
FROM cr.eu-north1.nebius.cloud/ml-containers/neubuntu:noble-20260106134848 AS slurm_check_job

ARG SLURM_VERSION
ARG PYXIS_VERSION=0.21.0

# Install dependencies
RUN apt-get update && \
    apt -y install \
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
        lsof && \
    apt clean && \
    rm -rf /var/lib/apt/lists/*

# Install slurm packages
RUN apt-get update && \
    apt -y install \
      slurm-smd-client=${SLURM_VERSION}-1 \
      slurm-smd-dev=${SLURM_VERSION}-1 \
      slurm-smd-libnss-slurm=${SLURM_VERSION}-1 \
      slurm-smd=${SLURM_VERSION}-1 && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Install slurm сhroot plugin
COPY images/common/chroot-plugin/chroot.c /usr/src/chroot-plugin/
COPY images/common/scripts/install_chroot_plugin.sh /opt/bin/
RUN chmod +x /opt/bin/install_chroot_plugin.sh && \
    /opt/bin/install_chroot_plugin.sh && \
    rm /opt/bin/install_chroot_plugin.sh

# Install parallel because it's required for enroot operation
RUN apt-get update && \
    apt -y install parallel=20240222+ds-2 && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Install enroot
COPY images/common/scripts/install_enroot.sh /opt/bin/
RUN chmod +x /opt/bin/install_enroot.sh && \
    /opt/bin/install_enroot.sh && \
    rm /opt/bin/install_enroot.sh

# Copy enroot configuration
COPY images/common/enroot/enroot.conf /etc/enroot/
COPY images/common/enroot/custom-dirs.conf /etc/enroot/enroot.conf.d/
RUN chown 0:0 /etc/enroot/enroot.conf && \
    chmod 644 /etc/enroot/enroot.conf && \
    chown 0:0 /etc/enroot/enroot.conf.d/custom-dirs.conf && \
    chmod 644 /etc/enroot/enroot.conf.d/custom-dirs.conf

# Install slurm pyxis plugin \
RUN apt-get update && \
    apt -y install nvslurm-plugin-pyxis=${SLURM_VERSION}-${PYXIS_VERSION}-1 && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Install kubectl
RUN ARCH="$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/')" && \
    KUBECTL_VERSION="$(curl -Ls https://dl.k8s.io/release/stable.txt)" && \
    echo "Downloading kubectl from https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${ARCH}/kubectl" && \
    curl -LO "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${ARCH}/kubectl" && \
    install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl && \
    rm kubectl

# Create single folder with slurm plugins for all architectures
RUN mkdir -p /usr/lib/slurm && \
    for dir in /usr/lib/*-linux-gnu/slurm; do \
      [ -d "$dir" ] && ln -sf $dir/* /usr/lib/slurm/ 2>/dev/null || true; \
    done

# Update linker cache
RUN ldconfig

# Delete users & home because they will be linked from jail
RUN rm /etc/passwd* /etc/group* /etc/shadow* /etc/gshadow*
RUN rm -rf /home

# Copy & run the entrypoint script
COPY images/slurm_check_job/slurm_check_job_entrypoint.sh /opt/bin/slurm/
COPY images/slurm_check_job/slurm_submit_jobs.sh /opt/bin/slurm/
RUN chmod +x /opt/bin/slurm/slurm_check_job_entrypoint.sh \
    && chmod +x /opt/bin/slurm/slurm_submit_jobs.sh

ENTRYPOINT ["/opt/bin/slurm/slurm_check_job_entrypoint.sh"]
