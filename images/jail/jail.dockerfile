FROM ubuntu:22.04 AS cuda

ENV DEBIAN_FRONTEND=noninteractive
ENV TZ=Etc/UTC
ENV LANG=en_US.UTF-8

RUN apt-get update &&  \
    apt-get install -y --no-install-recommends \
      gnupg2  \
      ca-certificates \
      locales \
      tzdata \
      wget && \
    wget https://developer.download.nvidia.com/compute/cuda/repos/ubuntu2204/x86_64/cuda-keyring_1.1-1_all.deb && \
    dpkg -i cuda-keyring_1.1-1_all.deb && \
    rm -rf cuda-keyring_1.1-1_all.deb && \
    ln -snf /usr/share/zoneinfo/Etc/UTC /etc/localtime && \
    locale-gen en_US.UTF-8 && \
    dpkg-reconfigure locales tzdata && \
    apt clean

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

ENV PATH=/usr/local/nvidia/bin:/usr/local/cuda/bin:${PATH}
ENV LD_LIBRARY_PATH=/usr/local/nvidia/lib:/usr/local/nvidia/lib64

# nvidia-container-runtime
ENV NVIDIA_VISIBLE_DEVICES=all
ENV NVIDIA_DRIVER_CAPABILITIES=compute,utility

# download and install mock packages for nvidia drivers https://github.com/nebius/soperator/issues/384
ARG PACKAGES_REPO_URL="https://github.com/nebius/slurm-deb-packages/releases/download"
RUN for pkg in cuda-drivers_9999.9999.9999_amd64.deb nvidia-open_9999.9999.9999_amd64.deb; do \
        wget -q -P /tmp "$PACKAGES_REPO_URL/cuda_mocks/${pkg}" && \
        echo "${pkg} successfully downloaded" || { echo "Failed to download ${pkg}"; exit 1; }; \
        dpkg -i "/tmp/${pkg}" && \
        rm -rf "/tmp/${pkg}"; \
    done

# About CUDA packages https://docs.nvidia.com/cuda/cuda-installation-guide-linux/#meta-packages
RUN apt update && \
    apt install -y \
        cuda=12.4.1-1 \
        libcublas-dev-12-4 \
        libcudnn9-cuda-12=9.1.0.70-1 \
        libcudnn9-dev-cuda-12=9.1.0.70-1 \
        libnccl-dev=2.21.5-1+cuda12.4 \
        libnccl2=2.21.5-1+cuda12.4 && \
    apt clean
COPY jail/pin_packages/cuda-pins /etc/apt/preferences.d/
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
RUN wget -P /tmp $PACKAGES_REPO_URL/nccl_tests_$CUDA_VERSION/nccl-tests-perf.tar.gz && \
    tar -xvzf /tmp/nccl-tests-perf.tar.gz -C /usr/bin && \
    rm -rf /tmp/nccl-tests-perf.tar.gz

#######################################################################################################################

FROM cuda AS jail

ARG SLURM_VERSION=24.05.5
ARG CUDA_VERSION=12.4.1
ARG PACKAGES_REPO_URL="https://github.com/nebius/slurm-deb-packages/releases/download"
ARG GDRCOPY_VERSION=2.4.4

ARG DEBIAN_FRONTEND=noninteractive

# TODO: Install only those dependencies that are required for Slurm client + useful utilities
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
        libpmix-dev && \
    apt clean

# Install python
COPY common/scripts/install_python.sh /opt/bin/
RUN chmod +x /opt/bin/install_python.sh && \
    /opt/bin/install_python.sh && \
    rm /opt/bin/install_python.sh

# Install parallel because it's required for enroot operation
COPY common/scripts/install_parallel.sh /opt/bin/
RUN chmod +x /opt/bin/install_parallel.sh && \
    /opt/bin/install_parallel.sh && \
    rm /opt/bin/install_parallel.sh

# Install OpenMPI
COPY common/scripts/install_openmpi.sh /opt/bin/
RUN chmod +x /opt/bin/install_openmpi.sh && \
    /opt/bin/install_openmpi.sh && \
    rm /opt/bin/install_openmpi.sh

# TODO: Install only necessary packages
# Download and install Slurm packages
RUN for pkg in slurm-smd-client slurm-smd-dev slurm-smd-libnss-slurm slurm-smd-libslurm-perl slurm-smd; do \
        wget -q -P /tmp $PACKAGES_REPO_URL/$CUDA_VERSION-$(grep 'VERSION_CODENAME' /etc/os-release | cut -d= -f2)-slurm$SLURM_VERSION/${pkg}_$SLURM_VERSION-1_amd64.deb && \
        echo "${pkg}_$SLURM_VERSION-1_amd64.deb successfully downloaded" || \
        { echo "Failed to download ${pkg}_$SLURM_VERSION-1_amd64.deb"; exit 1; }; \
    done

RUN apt install -y /tmp/*.deb && \
    rm -rf /tmp/*.deb && \
    apt clean

# Create directory for bind-mounting it from the host. It's needed for sbatch to work
RUN mkdir -m 755 -p /var/spool/slurmd

# Install nvidia-container-toolkit
COPY common/scripts/install_container_toolkit.sh /opt/bin/
RUN chmod +x /opt/bin/install_container_toolkit.sh && \
    /opt/bin/install_container_toolkit.sh && \
    rm /opt/bin/install_container_toolkit.sh

# Copy NVIDIA Container Toolkit config
COPY common/nvidia-container-runtime/config.toml /etc/nvidia-container-runtime/config.toml

# Install nvtop GPU monitoring utility
RUN add-apt-repository ppa:flexiondotorg/nvtop && \
    apt install -y nvtop && \
    apt clean

# Install dcgmi tools
# https://docs.nvidia.com/datacenter/dcgm/latest/user-guide/dcgm-diagnostics.html
RUN apt install -y datacenter-gpu-manager-4-cuda12 && \
    apt clean

# Install GDRCopy libraries & executables
RUN wget -q -P /tmp ${PACKAGES_REPO_URL}/gdrcopy-${GDRCOPY_VERSION}/gdrcopy_${GDRCOPY_VERSION}_amd64.Ubuntu22_04.deb || { echo "Failed to download gdrcopy"; exit 1; } && \
    wget -q -P /tmp ${PACKAGES_REPO_URL}/gdrcopy-${GDRCOPY_VERSION}/gdrcopy-tests_${GDRCOPY_VERSION}_amd64.Ubuntu22_04+cuda12.4.deb || { echo "Failed to download gdrcopy-tests"; exit 1; } && \
    wget -q -P /tmp ${PACKAGES_REPO_URL}/gdrcopy-${GDRCOPY_VERSION}/libgdrapi_${GDRCOPY_VERSION}_amd64.Ubuntu22_04.deb || { echo "Failed to download libgdrapi"; exit 1; } && \
    apt install -y /tmp/*.deb && \
    rm -rf /tmp/*.deb && \
    apt clean

# Install AWS CLI
COPY common/scripts/install_awscli.sh /opt/bin/
RUN chmod +x /opt/bin/install_awscli.sh && \
    /opt/bin/install_awscli.sh && \
    rm /opt/bin/install_awscli.sh

# Install Rclone
COPY common/scripts/install_rclone.sh /opt/bin/
RUN chmod +x /opt/bin/install_rclone.sh && \
    /opt/bin/install_rclone.sh && \
    rm /opt/bin/install_rclone.sh

# Install Docker CLI
COPY common/scripts/install_docker_cli.sh /opt/bin/
RUN chmod +x /opt/bin/install_docker_cli.sh && \
    /opt/bin/install_docker_cli.sh && \
    rm /opt/bin/install_docker_cli.sh

# Replace the real Docker CLI with a wrapper script
RUN mv /usr/bin/docker /usr/bin/docker.real
COPY jail/scripts/docker.sh /usr/bin/docker
RUN chmod +x /usr/bin/docker

# Create directory for pivoting host's root
RUN mkdir -m 555 /mnt/host

# Copy initial users
RUN rm /etc/passwd* /etc/group* /etc/shadow* /etc/gshadow*
COPY jail/init-users/* /etc/
RUN chmod 644 /etc/passwd /etc/group && chown 0:0 /etc/passwd /etc/group && \
    chmod 640 /etc/shadow /etc/gshadow && chown 0:42 /etc/shadow /etc/gshadow && \
    chmod 440 /etc/sudoers && chown 0:0 /etc/sudoers

# Setup the default $HOME directory content
RUN rm -rf -- /etc/skel/..?* /etc/skel/.[!.]* /etc/skel/*
COPY jail/skel/ /etc/skel/
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
COPY jail/scripts/createuser.sh /usr/bin/createuser
RUN chmod +x /usr/bin/createuser

# Replace SSH "message of the day" scripts
RUN rm -rf /etc/update-motd.d/*
COPY jail/motd/ /etc/update-motd.d/
RUN chmod +x /etc/update-motd.d/*

# Update linker cache
RUN ldconfig
