FROM cr.eu-north1.nebius.cloud/soperator/cuda_base:12.9.0-ubuntu24.04-nccl2.26.5-1-295cb71 AS cuda

# Download NCCL tests executables
ARG CUDA_VERSION=12.9.0
ARG PACKAGES_REPO_URL="https://github.com/nebius/slurm-deb-packages/releases/download"
RUN ARCH=$(uname -m) && \
    case "$ARCH" in \
      x86_64) ARCH_DEB=x64 ;; \
      aarch64) ARCH_DEB=arm64 ;; \
      *) echo "Unsupported architecture: $ARCH" && exit 1 ;; \
    esac && \
    echo "Using architecture: $ARCH_DEB" && \
    wget -P /tmp "${PACKAGES_REPO_URL}/nccl_tests_${CUDA_VERSION}_ubuntu24.04/nccl-tests-perf-${ARCH_DEB}.tar.gz" && \
    tar -xvzf /tmp/nccl-tests-perf-${ARCH_DEB}.tar.gz -C /usr/bin && \
    rm -rf /tmp/nccl-tests-perf-${ARCH_DEB}.tar.gz

#######################################################################################################################

FROM cuda AS jail

ARG SLURM_VERSION=24.11.6
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
        python3-apt \
        squashfs-tools \
        iputils-ping \
        dnsutils \
        telnet \
        netcat-openbsd \
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
        bsdmainutils \
        kmod \
        tmux \
        time \
        aptitude && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Install python
COPY images/common/scripts/install_python.sh /opt/bin/
RUN chmod +x /opt/bin/install_python.sh && \
    /opt/bin/install_python.sh && \
    rm /opt/bin/install_python.sh

# Install parallel because it's required for enroot operation
RUN apt-get update && \
    apt -y install parallel=20240222+ds-2 && \
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
    rm -rf /etc/slurm && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Create directory for bind-mounting it from the host. It's needed for sbatch to work
RUN mkdir -m 755 -p /var/spool/slurmd

# Install nvidia-container-toolkit (for enroot usage)
COPY images/common/scripts/install_container_toolkit.sh /opt/bin/
RUN chmod +x /opt/bin/install_container_toolkit.sh && \
    /opt/bin/install_container_toolkit.sh && \
    rm /opt/bin/install_container_toolkit.sh

# Copy NVIDIA Container Toolkit config
COPY images/common/nvidia-container-runtime/config.toml /etc/nvidia-container-runtime/config.toml

# Install nvtop GPU monitoring utility
RUN add-apt-repository -y ppa:quentiumyt/nvtop && \
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

# Copy Soperator utility scripts and add them to $PATH
COPY images/jail/scripts/soperator_instance_login.sh /opt/soperator_utils/soperator_instance_login.sh
COPY images/jail/scripts/slurm_task_info.sh /opt/soperator_utils/slurm_task_info.sh
COPY images/jail/scripts/worker_nvidia_bug_report.sh /opt/soperator_utils/worker_nvidia_bug_report.sh
COPY images/jail/scripts/fs_usage.sh /opt/soperator_utils/fs_usage.sh
RUN chmod -R 755 /opt/soperator_utils && \
    echo 'export PATH="/opt/soperator_utils:$PATH"' > /etc/profile.d/path_soperator_utils.sh && \
    chmod 755 /etc/profile.d/path_soperator_utils.sh

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
RUN chmod 755 /etc/skel/.ssh && \
    chmod 600 /etc/skel/.ssh/config && \
    chmod 755 /etc/skel/.slurm && \
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
COPY images/jail/scripts/createuser.py /usr/bin/createuser
RUN chmod +x /usr/bin/createuser

# Replace SSH "message of the day" scripts
RUN rm -rf /etc/update-motd.d/*
COPY images/jail/motd/ /etc/update-motd.d/
RUN chmod +x /etc/update-motd.d/*

# Save the initial jail version to a file
COPY VERSION /etc/soperator-jail-version

# Moved down to reduce build time
ARG NC_HEALTH_CHECKER=1.0.0-150.250826

# Install Nebius health-check library
RUN apt-get update && \
    apt-get install -y nc-health-checker=${NC_HEALTH_CHECKER} && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Update linker cache
RUN ldconfig
