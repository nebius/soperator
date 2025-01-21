ARG BASE_IMAGE=nvidia/cuda:12.4.1-cudnn-devel-ubuntu22.04

FROM $BASE_IMAGE AS jail

ARG SLURM_VERSION=24.05.5
ARG CUDA_VERSION=12.4.1
ARG OPENMPI_VERSION=4.1.7a1

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
        libopenmpi-dev \
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
        rdma-core \
        ibverbs-utils \
        libpmix2 \
        libpmix-dev

# Install python
COPY common/scripts/install_python.sh /opt/bin/
RUN chmod +x /opt/bin/install_python.sh && \
    /opt/bin/install_python.sh && \
    rm /opt/bin/install_python.sh

# Install mpi4py
RUN pip install -U pip wheel build && pip install mpi4py

# Install parallel because it's required for enroot operation
COPY common/scripts/install_parallel.sh /opt/bin/
RUN chmod +x /opt/bin/install_parallel.sh && \
    /opt/bin/install_parallel.sh && \
    rm /opt/bin/install_parallel.sh

# Install enroot
COPY common/scripts/install_enroot.sh /opt/bin/
RUN chmod +x /opt/bin/install_enroot.sh && \
    /opt/bin/install_enroot.sh && \
    rm /opt/bin/install_enroot.sh

# Copy enroot configuration
COPY jail/enroot/enroot.conf /etc/enroot/
RUN chown 0:0 /etc/enroot/enroot.conf && chmod 644 /etc/enroot/enroot.conf

# Create directory for enroot runtime data that will be mounted from the host
RUN mkdir -p -m 777 /usr/share/enroot/enroot-data

# Install OpenMPI
COPY common/scripts/install_openmpi.sh /opt/bin/
RUN chmod +x /opt/bin/install_openmpi.sh && \
    /opt/bin/install_openmpi.sh && \
    rm /opt/bin/install_openmpi.sh

ENV LD_LIBRARY_PATH=/lib/x86_64-linux-gnu:/usr/lib/x86_64-linux-gnu:/usr/local/nvidia/lib:/usr/local/nvidia/lib64:/usr/local/cuda/targets/x86_64-linux/lib:/usr/mpi/gcc/openmpi-${OPENMPI_VERSION}/lib
ENV PATH=$PATH:/usr/mpi/gcc/openmpi-${OPENMPI_VERSION}/bin

# TODO: Install only necessary packages
# Download and install Slurm packages
RUN for pkg in slurm-smd-client slurm-smd-dev slurm-smd-libnss-slurm slurm-smd-libslurm-perl slurm-smd; do \
        wget -q -P /tmp https://github.com/nebius/slurm-deb-packages/releases/download/$CUDA_VERSION-$(grep 'VERSION_CODENAME' /etc/os-release | cut -d= -f2)-slurm$SLURM_VERSION/${pkg}_$SLURM_VERSION-1_amd64.deb && \
        echo "${pkg}_$SLURM_VERSION-1_amd64.deb successfully downloaded" || \
        { echo "Failed to download ${pkg}_$SLURM_VERSION-1_amd64.deb"; exit 1; }; \
    done

RUN apt install -y /tmp/*.deb && rm -rf /tmp/*.deb

# Install slurm pyxis plugin
COPY common/scripts/install_pyxis_plugin.sh /opt/bin/
RUN chmod +x /opt/bin/install_pyxis_plugin.sh && \
    /opt/bin/install_pyxis_plugin.sh && \
    rm /opt/bin/install_pyxis_plugin.sh

# Create directory for bind-mounting it from the host. It's needed for sbatch to work
RUN mkdir -m 755 -p /var/spool/slurmd

# Install nvidia-container-toolkit
COPY common/scripts/install_container_toolkit.sh /opt/bin/
RUN chmod +x /opt/bin/install_container_toolkit.sh && \
    /opt/bin/install_container_toolkit.sh && \
    rm /opt/bin/install_container_toolkit.sh

# Install nvtop GPU monitoring utility
COPY common/scripts/install_nvtop.sh /opt/bin/
RUN chmod +x /opt/bin/install_nvtop.sh && \
    /opt/bin/install_nvtop.sh && \
    rm /opt/bin/install_nvtop.sh

# Download NCCL tests executables
RUN wget -P /tmp https://github.com/nebius/slurm-deb-packages/releases/download/$CUDA_VERSION-$(grep 'VERSION_CODENAME' /etc/os-release | cut -d= -f2)-slurm$SLURM_VERSION/nccl-tests-perf.tar.gz && \
    tar -xvzf /tmp/nccl-tests-perf.tar.gz -C /usr/bin && \
    rm -rf /tmp/nccl-tests-perf.tar.gz

# Install GDRCopy libraries & executables
COPY common/scripts/install_gdrcopy.sh /opt/bin/
RUN chmod +x /opt/bin/install_gdrcopy.sh && \
    /opt/bin/install_gdrcopy.sh && \
    rm /opt/bin/install_gdrcopy.sh

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
    chmod 644 /etc/skel/.profile

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
