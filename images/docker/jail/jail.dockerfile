FROM nvidia/cuda:12.2.2-cudnn8-devel-ubuntu20.04 as jail

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
        infiniband-diags

# Install python
COPY docker/common/scripts/install_python.sh /opt/bin/
RUN chmod +x /opt/bin/install_python.sh && \
    /opt/bin/install_python.sh && \
    rm /opt/bin/install_python.sh

# Install mpi4py
RUN pip install mpi4py

# Install parallel because it's required for enroot operation
COPY docker/common/scripts/install_parallel.sh /opt/bin/
RUN chmod +x /opt/bin/install_parallel.sh && \
    /opt/bin/install_parallel.sh && \
    rm /opt/bin/install_parallel.sh

# Install enroot
COPY docker/common/scripts/install_enroot.sh /opt/bin/
RUN chmod +x /opt/bin/install_enroot.sh && \
    /opt/bin/install_enroot.sh && \
    rm /opt/bin/install_enroot.sh

# Copy enroot configuration
COPY docker/jail/enroot-conf/enroot.conf /etc/enroot/
RUN chown 0:0 /etc/enroot/enroot.conf && chmod 644 /etc/enroot/enroot.conf

# Create directory for enroot runtime data that will be mounted from the host
RUN mkdir -p -m 777 /usr/share/enroot/enroot-data

# Install PMIx
COPY docker/common/scripts/install_pmix.sh /opt/bin/
RUN chmod +x /opt/bin/install_pmix.sh && \
    /opt/bin/install_pmix.sh && \
    rm /opt/bin/install_pmix.sh

# TODO: Install only necessary packages
# Copy and install Slurm packages
COPY --from=slurm /usr/src/slurm-smd-client_23.11.6-1_amd64.deb /tmp/
COPY --from=slurm /usr/src/slurm-smd-dev_23.11.6-1_amd64.deb /tmp/
COPY --from=slurm /usr/src/slurm-smd-libnss-slurm_23.11.6-1_amd64.deb /tmp/
COPY --from=slurm /usr/src/slurm-smd-libpmi0_23.11.6-1_amd64.deb /tmp/
COPY --from=slurm /usr/src/slurm-smd-libpmi2-0_23.11.6-1_amd64.deb /tmp/
COPY --from=slurm /usr/src/slurm-smd-libslurm-perl_23.11.6-1_amd64.deb /tmp/
COPY --from=slurm /usr/src/slurm-smd-openlava_23.11.6-1_all.deb /tmp/
COPY --from=slurm /usr/src/slurm-smd_23.11.6-1_amd64.deb /tmp/
RUN apt install -y /tmp/*.deb && rm -rf /tmp/*.deb

# Install slurm plugins
COPY docker/common/chroot-plugin/chroot.c /usr/src/chroot-plugin/
COPY docker/common/scripts/install_slurm_plugins.sh /opt/bin/
RUN chmod +x /opt/bin/install_slurm_plugins.sh && \
    /opt/bin/install_slurm_plugins.sh && \
    rm /opt/bin/install_slurm_plugins.sh

# Create directory for bind-mounting it from the host. It's needed for sbatch to work
RUN mkdir -m 755 -p /var/spool/slurmd

# Install nvidia-container-toolkit
COPY docker/common/scripts/install_container_toolkit.sh /opt/bin/
RUN chmod +x /opt/bin/install_container_toolkit.sh && \
    /opt/bin/install_container_toolkit.sh && \
    rm /opt/bin/install_container_toolkit.sh

# Copy and install NCCL packages
COPY --from=nccl /usr/src/nccl/build/pkg/deb/*.deb /tmp/
RUN dpkg -i /tmp/libnccl2_2.21.5-1+cuda12.2_amd64.deb && \
    dpkg -i /tmp/libnccl-dev_2.21.5-1+cuda12.2_amd64.deb && \
    rm -rf /tmp/*.deb

# Copy NCCL tests executables
COPY --from=nccl_tests /usr/src/nccl-tests/build/*_perf /usr/bin/

# Copy script that performs GPU benchmark
COPY docker/jail/scripts/srun_perf_run.sh /usr/bin/srun_perf_run.sh
RUN chmod +x /usr/bin/srun_perf_run.sh

# Create directory for pivoting host's root
RUN mkdir -m 555 /mnt/host

# Copy initial users
RUN rm /etc/passwd* /etc/group* /etc/shadow* /etc/gshadow*
COPY docker/jail/init-users/* /etc/
RUN chmod 644 /etc/passwd /etc/group && chown 0:0 /etc/passwd /etc/group && \
    chmod 640 /etc/shadow /etc/gshadow && chown 0:42 /etc/shadow /etc/gshadow

# Adjust the default $HOME directory content
RUN cd /etc/skel && \
    mkdir -m 755 .slurm && \
    touch .slurm/defaults && \
    chmod 644 .slurm/defaults && \
    cp -r /etc/skel/.slurm /root/

# Copy createuser utility script
COPY docker/jail/scripts/createuser.sh /usr/bin/createuser
RUN chmod +x /usr/bin/createuser

# Update linker cache
RUN ldconfig
