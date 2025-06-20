ARG BASE_IMAGE=cr.eu-north1.nebius.cloud/soperator/ubuntu:jammy

# First stage: Build the prometheus-slurm-exporter from source
FROM golang:1.22 AS exporter_builder

ARG GO_LDFLAGS=""
ARG CGO_ENABLED=0
ARG GOOS=linux
ARG VERSION_EXPORTER=0.20

RUN apt-get update && \
    apt -y install \
        wget \
        unzip && \
    apt clean

WORKDIR /app

RUN wget https://github.com/vpenso/prometheus-slurm-exporter/archive/refs/tags/${VERSION_EXPORTER}.zip -O /app/prometheus-slurm-exporter.zip && \
    unzip /app/prometheus-slurm-exporter.zip -d /app

WORKDIR /app/prometheus-slurm-exporter-${VERSION_EXPORTER}

RUN GOOS=$GOOS CGO_ENABLED=$CGO_ENABLED GO_LDFLAGS=$GO_LDFLAGS \
    go build -o prometheus-slurm-exporter . && \
    mv prometheus-slurm-exporter /app/

#######################################################################################################################
# Second stage: Build image for the prometheus-slurm-exporter
FROM $BASE_IMAGE AS exporter

ARG SLURM_VERSION=24.11.5
# ARCH has the short form like: amd64, arm64
ARG ARCH
# ALT_ARCH has the extended form like: x86_64, aarch64
ARG ALT_ARCH

# Install dependencies
RUN apt-get update && \
    apt -y install \
        wget \
        git \
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
        pciutils \
        iproute2 \
        kmod \
        daemontools \
        libncurses5-dev \
        libdrm-dev && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Add Nebius public registry
RUN curl -fsSL https://dr.nebius.cloud/public.gpg -o /usr/share/keyrings/nebius.gpg.pub && \
    echo "deb [signed-by=/usr/share/keyrings/nebius.gpg.pub] https://dr.nebius.cloud/ stable main" > /etc/apt/sources.list.d/nebius.list

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
    ALT_ARCH=${ALT_ARCH} /opt/bin/install_chroot_plugin.sh && \
    rm /opt/bin/install_chroot_plugin.sh

# Install NCCL debug plugin
COPY images/common/spank-nccl-debug/src /usr/src/soperator/spank/nccld-debug
COPY images/common/scripts/install_nccld_debug_plugin.sh /opt/bin/
RUN chmod +x /opt/bin/install_nccld_debug_plugin.sh && \
    ALT_ARCH=${ALT_ARCH} /opt/bin/install_nccld_debug_plugin.sh && \
    rm /opt/bin/install_nccld_debug_plugin.sh

# Update linker cache
RUN ldconfig

COPY --from=exporter_builder /app/prometheus-slurm-exporter /opt/bin/

ENTRYPOINT ["/opt/bin/prometheus-slurm-exporter"]
