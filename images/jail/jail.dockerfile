FROM cr.eu-north1.nebius.cloud/soperator/cuda_base:12.9.0-ubuntu24.04-nccl2.26.5-1-295cb71 AS cuda

# Download NCCL tests executables
ARG CUDA_VERSION=12.9.0
ARG PACKAGES_REPO_URL="https://github.com/nebius/slurm-deb-packages/releases/download"
RUN ARCH=$(uname -m) && \
    echo "Using architecture: ${ARCH}" && \
    wget -P /tmp "${PACKAGES_REPO_URL}/nccl_tests_${CUDA_VERSION}_ubuntu24.04/nccl-tests-perf-${ARCH}.tar.gz" && \
    tar -xvzf /tmp/nccl-tests-perf-${ARCH}.tar.gz -C /usr/bin && \
    rm -rf /tmp/nccl-tests-perf-${ARCH}.tar.gz

#######################################################################################################################

FROM cuda AS jail

ARG DEBIAN_FRONTEND=noninteractive

# Install dependencies
RUN apt update && \
    apt install -y \
        autoconf="2.71-3" \
        bc \
        build-essential="12.10ubuntu1" \
        ca-certificates="20240203" \
        curl \
        flex \
        gettext-base \
        git \
        gnupg \
        jq \
        less \
        libevent-dev="2.1.12-stable-9ubuntu2" \
        libhwloc-dev="2.10.0-1build1" \
        libjansson-dev="2.14-2build2" \
        libjson-c-dev="0.17-1build1" \
        liblz4-dev="1.9.4-1build1.1" \
        libmunge-dev="0.5.15-4build1" \
        libpam0g-dev="1.5.3-5ubuntu5.5" \
        libssl-dev="3.0.13-0ubuntu3.6" \
        libtool="2.4.7-7build1" \
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
        rdma-core="50.0-2ubuntu0.2" \
        ibverbs-utils="50.0-2ubuntu0.2" \
        libpmix-dev="5.0.1-4.1build1" \
        parallel="20240222+ds-2" \
        bsdmainutils \
        kmod \
        tmux \
        time \
        aptitude && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

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

# Replace SSH "message of the day" scripts
RUN rm -rf /etc/update-motd.d/*
COPY images/jail/motd/ /etc/update-motd.d/
RUN chmod +x /etc/update-motd.d/*

# Install python
RUN apt-get update && \
    apt-get install -y \
        python3.12="3.12.3-1ubuntu0.8" \
        python3.12-dev="3.12.3-1ubuntu0.8" \
        python3.12-venv="3.12.3-1ubuntu0.8" \
        python3.12-dbg="3.12.3-1ubuntu0.8" \
        python3-pip="24.0+dfsg-1ubuntu1.3" \
        python3-pip-whl="24.0+dfsg-1ubuntu1.3" \
        python3-debian="0.1.49ubuntu2" && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/* && \
    ln -sf /usr/bin/python3.12 /usr/bin/python && \
    ln -sf /usr/bin/python3.12 /usr/bin/python3

# Install OpenMPI
COPY images/common/scripts/install_openmpi.sh /opt/bin/
RUN chmod +x /opt/bin/install_openmpi.sh && \
    /opt/bin/install_openmpi.sh && \
    rm /opt/bin/install_openmpi.sh

# Install nvtop GPU monitoring utility
RUN add-apt-repository -y ppa:quentiumyt/nvtop && \
    apt install -y nvtop="3.2.0.2-1+noble" && \
    apt clean && \
    rm -rf /var/lib/apt/lists/*

# Install dcgmi tools
# https://docs.nvidia.com/datacenter/dcgm/latest/user-guide/dcgm-diagnostics.html
RUN apt-get update && \
    apt install -y datacenter-gpu-manager-4-cuda12="1:4.4.1-1" && \
    apt clean && \
    rm -rf /var/lib/apt/lists/*

# Install GDRCopy libraries & executables
ARG GDRCOPY_VERSION=2.5
RUN apt-get update && \
    apt -y install \
      gdrcopy=${GDRCOPY_VERSION}-1 \
      gdrcopy-tests=${GDRCOPY_VERSION}-1 \
      libgdrapi=${GDRCOPY_VERSION}-1 && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Install nvidia-container-toolkit (for enroot usage)
COPY images/common/scripts/install_container_toolkit.sh /opt/bin/
RUN chmod +x /opt/bin/install_container_toolkit.sh && \
    /opt/bin/install_container_toolkit.sh && \
    rm /opt/bin/install_container_toolkit.sh

# Copy NVIDIA Container Toolkit config
COPY images/common/nvidia-container-runtime/config.toml /etc/nvidia-container-runtime/config.toml

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

# Copy soperator-createuser utility script
COPY images/jail/scripts/soperator-createuser.py /usr/bin/soperator-createuser
RUN chmod +x /usr/bin/soperator-createuser && \
    ln -sf /usr/bin/soperator-createuser /usr/bin/createuser

ARG SLURM_VERSION
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

# Create single folder with slurm plugins for all architectures
RUN mkdir -p /usr/lib/slurm && \
    for dir in /usr/lib/*-linux-gnu/slurm; do \
      [ -d "$dir" ] && ln -sf $dir/* /usr/lib/slurm/ 2>/dev/null || true; \
    done

# Divert files for correct slurm package upgrades and mounting them from container
RUN set -eux; \
    libdir="/usr/lib/$(uname -m)-linux-gnu"; \
    dpkg-divert --add --local --no-rename "$(find "$libdir" -maxdepth 1 -type f -name 'libslurm.so.*.0.0')"; \
    dpkg-divert --add --local --no-rename "$libdir/libnss_slurm.so.2"; \
    dpkg-divert --add --local --no-rename /usr/share/bash-completion/completions/slurm_completion.sh; \
    for b in sacct salloc sbatch scancel scrontab sdiag sinfo squeue srun sstat \
             sacctmgr sattach sbcast scontrol scrun sh5util sprio sreport sshare strigger; do \
        dpkg-divert --add --local --no-rename "/usr/bin/$b"; \
    done


# Install Nebius health-check library
ARG NC_HEALTH_CHECKER=1.0.0-162.251030
RUN apt-get update && \
    apt-get install -y nc-health-checker=${NC_HEALTH_CHECKER} && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Save the initial jail version to a file
COPY VERSION /etc/soperator-jail-version

# Update linker cache
RUN ldconfig
