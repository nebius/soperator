ARG BASE_IMAGE=cr.eu-north1.nebius.cloud/soperator/ubuntu:noble

FROM $BASE_IMAGE AS slurm_check_job

ARG SLURM_VERSION=24.11.5
ARG PYXIS_VERSION=0.21.0
# ARCH has the short form like: amd64, arm64
ARG ARCH
# ALT_ARCH has the extended form like: x86_64, aarch64
ARG ALT_ARCH

ARG DEBIAN_FRONTEND=noninteractive

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
        lsof && \
    apt clean

# Add Nebius public registry
RUN curl -fsSL https://dr.nebius.cloud/public.gpg -o /usr/share/keyrings/nebius.gpg.pub && \
    codename="$(. /etc/os-release && echo $VERSION_CODENAME)" && \
    echo "deb [signed-by=/usr/share/keyrings/nebius.gpg.pub] https://dr.nebius.cloud/ $codename main" > /etc/apt/sources.list.d/nebius.list && \
    echo "deb [signed-by=/usr/share/keyrings/nebius.gpg.pub] https://dr.nebius.cloud/ stable main" >> /etc/apt/sources.list.d/nebius.list

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

# Install parallel because it's required for enroot operation
RUN apt-get update && \
    apt -y install parallel=20210822+ds-2 && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Install enroot
COPY images/common/scripts/install_enroot.sh /opt/bin/
RUN chmod +x /opt/bin/install_enroot.sh && \
    /opt/bin/install_enroot.sh && \
    rm /opt/bin/install_enroot.sh

# Copy enroot configuration
COPY images/common/enroot/enroot.conf /etc/enroot/
RUN chown 0:0 /etc/enroot/enroot.conf && chmod 644 /etc/enroot/enroot.conf

# Install slurm pyxis plugin \
RUN apt-get update && \
    apt -y install nvslurm-plugin-pyxis=${SLURM_VERSION}-${PYXIS_VERSION}-1 && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Copy script for complementing jail filesystem in runtime
COPY images/common/scripts/complement_jail.sh /opt/bin/slurm/

# Copy script for bind-mounting slurm into the jail
COPY images/common/scripts/bind_slurm_common.sh /opt/bin/slurm/

RUN chmod +x /opt/bin/slurm/complement_jail.sh && \
    chmod +x /opt/bin/slurm/bind_slurm_common.sh

# Install kubectl
RUN KUBECTL_VERSION=$(curl -Ls https://dl.k8s.io/release/stable.txt) && \
    echo "Downloading kubectl from https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${ARCH}/kubectl" && \
    curl -LO https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${ARCH}/kubectl && \
    install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl && \
    rm kubectl

# Update linker cache
RUN ldconfig

# Delete users & home because they will be linked from jail
RUN rm /etc/passwd* /etc/group* /etc/shadow* /etc/gshadow*
RUN rm -rf /home

# Copy & run the entrypoint script
COPY images/slurm_check_job/slurm_check_job_entrypoint.sh /opt/bin/slurm/
COPY images/slurm_check_job/slurm_submit_array_job.sh /opt/bin/slurm/
RUN chmod +x /opt/bin/slurm/slurm_check_job_entrypoint.sh \
    && chmod +x /opt/bin/slurm/slurm_submit_array_job.sh

ENTRYPOINT ["/opt/bin/slurm/slurm_check_job_entrypoint.sh"]
