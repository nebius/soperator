ARG BASE_IMAGE=cr.eu-north1.nebius.cloud/soperator/ubuntu:jammy

# First stage: Build the prometheus-slurm-exporter from source
FROM golang:1.22 AS exporter_builder

ARG GO_LDFLAGS=""
ARG CGO_ENABLED=0
ARG GOOS=linux
ARG GOARCH=amd64
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

RUN GOOS=$GOOS GOARCH=$GOARCH CGO_ENABLED=$CGO_ENABLED GO_LDFLAGS=$GO_LDFLAGS \
    go build -o prometheus-slurm-exporter . && \
    mv prometheus-slurm-exporter /app/

#######################################################################################################################
# Second stage: Build image for the prometheus-slurm-exporter
FROM $BASE_IMAGE AS exporter

ARG SLURM_VERSION=24.05.5

# TODO: Install only those dependencies that are required for running slurm exporter
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
    apt clean

ARG PACKAGES_REPO_URL="https://github.com/nebius/slurm-deb-packages/releases/download"
# Download and install Slurm packages
RUN for pkg in slurm-smd-client slurm-smd-dev slurm-smd-libnss-slurm slurm-smd; do \
        wget -q -P /tmp $PACKAGES_REPO_URL/slurm-packages-$SLURM_VERSION/${pkg}_$SLURM_VERSION-1_amd64.deb && \
        echo "${pkg}_$SLURM_VERSION-1_amd64.deb successfully downloaded" || \
        { echo "Failed to download ${pkg}_$SLURM_VERSION-1_amd64.deb"; exit 1; }; \
    done && \
    apt install -y /tmp/*.deb && \
    rm -rf /tmp/*.deb && \
    apt clean

# Install slurm сhroot plugin
COPY common/chroot-plugin/chroot.c /usr/src/chroot-plugin/
COPY common/scripts/install_chroot_plugin.sh /opt/bin/
RUN chmod +x /opt/bin/install_chroot_plugin.sh && \
    /opt/bin/install_chroot_plugin.sh && \
    rm /opt/bin/install_chroot_plugin.sh

# Update linker cache
RUN ldconfig

COPY --from=exporter_builder /app/prometheus-slurm-exporter /opt/bin/

ENTRYPOINT ["/opt/bin/prometheus-slurm-exporter"]
