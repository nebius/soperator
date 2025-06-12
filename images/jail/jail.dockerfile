FROM cr.eu-north1.nebius.cloud/soperator/ubuntu:jammy AS cuda

ENV DEBIAN_FRONTEND=noninteractive
ENV TZ=Etc/UTC
ENV LANG=en_US.UTF-8

# ARCH has the short form like: amd64, arm64
ARG ARCH
# ALT_ARCH has the extended form like: x86_64, aarch64
ARG ALT_ARCH

RUN apt-get update &&  \
    apt-get install -y --no-install-recommends \
      gnupg2  \
      ca-certificates \
      locales \
      tzdata \
      wget \
      curl && \
    ARCH=$(uname -m) && \
        case "$ARCH" in \
          x86_64) ARCH_DEB=x86_64 ;; \
          aarch64) ARCH_DEB=sbsa ;; \
          *) echo "Unsupported architecture: ${ARCH}" && exit 1 ;; \
        esac && \
        echo "Using architecture: ${ARCH_DEB}" && \
    wget https://developer.download.nvidia.com/compute/cuda/repos/ubuntu2204/${ARCH_DEB}/cuda-keyring_1.1-1_all.deb && \
    dpkg -i cuda-keyring_1.1-1_all.deb && \
    rm -rf cuda-keyring_1.1-1_all.deb && \
    ln -snf /usr/share/zoneinfo/Etc/UTC /etc/localtime && \
    locale-gen en_US.UTF-8 && \
    dpkg-reconfigure locales tzdata && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

ENV LANG="en_US.UTF-8" \
	LC_CTYPE="en_US.UTF-8" \
	LC_NUMERIC="en_US.UTF-8" \
	LC_TIME="en_US.UTF-8" \
	LC_COLLATE="en_US.UTF-8" \
	LC_MONETARY="en_US.UTF-8" \
	LC_MESSAGES="en_US.UTF-8" \
	LC_PAPER="en_US.UTF-8" \
	LC_NAME="en_US.UTF-8" \
	LC_ADDRESS="en_US.UTF-8" \
	LC_TELEPHONE="en_US.UTF-8" \
	LC_MEASUREMENT="en_US.UTF-8" \
	LC_IDENTIFICATION="en_US.UTF-8"

ENV PATH=/usr/local/cuda/bin:${PATH}

# nvidia-container-runtime
ENV NVIDIA_VISIBLE_DEVICES=all
ENV NVIDIA_DRIVER_CAPABILITIES=compute,utility

# Add Nebius public registry
RUN curl -fsSL https://dr.nebius.cloud/public.gpg -o /usr/share/keyrings/nebius.gpg.pub && \
    echo "deb [signed-by=/usr/share/keyrings/nebius.gpg.pub] https://dr.nebius.cloud/ stable main" > /etc/apt/sources.list.d/nebius.list

# Install mock packages for nvidia drivers https://github.com/nebius/soperator/issues/384
RUN apt-get update && \
    apt -y install \
      cuda-drivers=9999.9999.9999 \
      nvidia-open=9999.9999.9999 && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# About CUDA packages https://docs.nvidia.com/cuda/cuda-installation-guide-linux/#meta-packages
RUN apt update && \
    apt install -y \
        cuda=12.4.1-1 \
        libcublas-dev-12-4 \
        libcudnn9-cuda-12=9.1.0.70-1 \
        libcudnn9-dev-cuda-12=9.1.0.70-1 \
        libnccl-dev=2.21.5-1+cuda12.4 \
        libnccl2=2.21.5-1+cuda12.4 && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

COPY images/jail/pin_packages/cuda-pins /etc/apt/preferences.d/
RUN apt update

RUN apt-mark hold \
      libcublas-12-4 \
      libcublas-dev-12-4 \
      libcudnn9-cuda-12 \
      libnccl-dev=2.21.5-1+cuda12.4 \
      libnccl2

RUN echo "export PATH=\$PATH:/usr/local/cuda/bin" > /etc/profile.d/path_cuda.sh && \
    . /etc/profile.d/path_cuda.sh

ENV LIBRARY_PATH=/usr/local/cuda/lib64/stubs

# Download NCCL tests executables
ARG CUDA_VERSION=12.4.1
ARG PACKAGES_REPO_URL="https://github.com/nebius/slurm-deb-packages/releases/download"
RUN ARCH=$(uname -m) && \
    case "$ARCH" in \
      x86_64) ARCH_DEB=x64 ;; \
      aarch64) ARCH_DEB=arm64 ;; \
      *) echo "Unsupported architecture: $ARCH" && exit 1 ;; \
    esac && \
    echo "Using architecture: $ARCH_DEB" && \
    wget -P /tmp $PACKAGES_REPO_URL/nccl_tests_$CUDA_VERSION/nccl-tests-perf-${ARCH_DEB}.tar.gz && \
    tar -xvzf /tmp/nccl-tests-perf-${ARCH_DEB}.tar.gz -C /usr/bin && \
    rm -rf /tmp/nccl-tests-perf-${ARCH_DEB}.tar.gz

#######################################################################################################################

FROM cuda AS jail

ARG SLURM_VERSION=24.05.7
ARG GDRCOPY_VERSION=2.5

ARG DEBIAN_FRONTEND=noninteractive

# Install dependencies
RUN apt update && \
    apt install -y \
        autoconf \
        bc \
        build-essential \
        curl \
        flex \
        gettext-base \
        git \
        gnupg \
        jq \
        less \
        libevent-dev \
        libhwloc-dev \
        libjansson-dev \
        libjson-c-dev \
        liblz4-dev \
        libmunge-dev \
        libpam0g-dev \
        libssl-dev \
        libtool \
        lsof \
        pkg-config \
        software-properties-common \
        squashfs-tools \
        iputils-ping \
        dnsutils \
        telnet \
        netcat \
        strace \
        sudo \
        tree \
        vim \
        wget \
        zstd \
        pciutils \
        iproute2 \
        infiniband-diags \
        libncurses5-dev \
        libdrm-dev \
        zip \
        unzip \
        rsync \
        numactl \
        htop \
        hwloc \
        rdma-core \
        ibverbs-utils \
        libpmix2 \
        libpmix-dev \
        bsdmainutils && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Install python
COPY images/common/scripts/install_python.sh /opt/bin/
RUN chmod +x /opt/bin/install_python.sh && \
    /opt/bin/install_python.sh && \
    rm /opt/bin/install_python.sh

# Install parallel because it's required for enroot operation
RUN apt-get update && \
    apt -y install parallel=20210822+ds-2 && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Install OpenMPI
COPY images/common/scripts/install_openmpi.sh /opt/bin/
RUN chmod +x /opt/bin/install_openmpi.sh && \
    /opt/bin/install_openmpi.sh && \
    rm /opt/bin/install_openmpi.sh

# Install Slurm packages
RUN apt-get update && \
    apt -y install \
      slurm-smd-client=${SLURM_VERSION}-1 \
      slurm-smd-dev=${SLURM_VERSION}-1 \
      slurm-smd-libnss-slurm=${SLURM_VERSION}-1 \
      slurm-smd=${SLURM_VERSION}-1 && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Create directory for bind-mounting it from the host. It's needed for sbatch to work
RUN mkdir -m 755 -p /var/spool/slurmd

# Install nvidia-container-toolkit
COPY images/common/scripts/install_container_toolkit.sh /opt/bin/
RUN chmod +x /opt/bin/install_container_toolkit.sh && \
    /opt/bin/install_container_toolkit.sh && \
    rm /opt/bin/install_container_toolkit.sh

# Copy NVIDIA Container Toolkit config
COPY images/common/nvidia-container-runtime/config.toml /etc/nvidia-container-runtime/config.toml

# Install nvtop GPU monitoring utility
RUN add-apt-repository ppa:flexiondotorg/nvtop && \
    apt-get update && \
    apt install -y nvtop && \
    apt clean && \
    rm -rf /var/lib/apt/lists/*

# Install dcgmi tools
# https://docs.nvidia.com/datacenter/dcgm/latest/user-guide/dcgm-diagnostics.html
RUN apt-get update && \
    apt install -y datacenter-gpu-manager-4-cuda12 && \
    apt clean && \
    rm -rf /var/lib/apt/lists/*

# Install GDRCopy libraries & executables
RUN apt-get update && \
    apt -y install \
      gdrcopy=${GDRCOPY_VERSION}-1 \
      gdrcopy-tests=${GDRCOPY_VERSION}-1 \
      libgdrapi=${GDRCOPY_VERSION}-1 && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Install AWS CLI
COPY images/common/scripts/install_awscli.sh /opt/bin/
RUN chmod +x /opt/bin/install_awscli.sh && \
    /opt/bin/install_awscli.sh && \
    rm /opt/bin/install_awscli.sh

# Install Rclone
COPY images/common/scripts/install_rclone.sh /opt/bin/
RUN chmod +x /opt/bin/install_rclone.sh && \
    /opt/bin/install_rclone.sh && \
    rm /opt/bin/install_rclone.sh

# Install Docker CLI
COPY images/common/scripts/install_docker_cli.sh /opt/bin/
RUN chmod +x /opt/bin/install_docker_cli.sh && \
    /opt/bin/install_docker_cli.sh && \
    rm /opt/bin/install_docker_cli.sh

# Replace the real Docker CLI with a wrapper script
RUN mv /usr/bin/docker /usr/bin/docker.real
COPY images/jail/scripts/docker.sh /usr/bin/docker
RUN chmod +x /usr/bin/docker

# Create a wrapper script for nvidia-smi that shows running processes (in the host's PID namespace)
COPY images/jail/scripts/nvidia_smi_hostpid.sh /usr/bin/nvidia-smi-hostpid
RUN chmod +x /usr/bin/nvidia-smi-hostpid

# Create directory for pivoting host's root
RUN mkdir -m 555 /mnt/host

# Copy initial users
RUN rm /etc/passwd* /etc/group* /etc/shadow* /etc/gshadow*
COPY images/jail/init-users/* /etc/
RUN chmod 644 /etc/passwd /etc/group && chown 0:0 /etc/passwd /etc/group && \
    chmod 640 /etc/shadow /etc/gshadow && chown 0:42 /etc/shadow /etc/gshadow && \
    chmod 440 /etc/sudoers && chown 0:0 /etc/sudoers

# Setup the default $HOME directory content
RUN rm -rf -- /etc/skel/..?* /etc/skel/.[!.]* /etc/skel/*
COPY images/jail/skel/ /etc/skel/
RUN chmod 755 /etc/skel/.slurm && \
    chmod 644 /etc/skel/.slurm/defaults && \
    chmod 644 /etc/skel/.bash_logout && \
    chmod 644 /etc/skel/.bashrc && \
    chmod 644 /etc/skel/.profile && \
    chmod 755 /etc/skel/.config && \
    chmod 755 /etc/skel/.config/enroot && \
    chmod 644 /etc/skel/.config/enroot/.credentials

# Use the same /etc/skel content for /root
RUN rm -rf -- /root/..?* /root/.[!.]* /root/* && \
    cp -a /etc/skel/. /root/

# Copy createuser utility script
COPY images/jail/scripts/createuser.sh /usr/bin/createuser
RUN chmod +x /usr/bin/createuser

# Replace SSH "message of the day" scripts
RUN rm -rf /etc/update-motd.d/*
COPY images/jail/motd/ /etc/update-motd.d/
RUN chmod +x /etc/update-motd.d/*

# Update linker cache
RUN ldconfig
