ARG BASE_IMAGE=ubuntu:jammy

FROM $BASE_IMAGE AS nccl_benchmark

ARG SLURM_VERSION=24.05.5
ARG CUDA_VERSION=12.4.1

ARG DEBIAN_FRONTEND=noninteractive

# TODO: Install only those dependencies that are required for running NCCL bacnhmark
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
        vim \
        libpmix2 \
        libpmix-dev

# TODO: Install only necessary packages
# Download and install Slurm packages
RUN for pkg in slurm-smd-client slurm-smd-dev slurm-smd-libnss-slurm slurm-smd-libslurm-perl slurm-smd; do \
        wget -q -P /tmp https://github.com/nebius/slurm-deb-packages/releases/download/$CUDA_VERSION-$(grep 'VERSION_CODENAME' /etc/os-release | cut -d= -f2)-slurm$SLURM_VERSION/${pkg}_$SLURM_VERSION-1_amd64.deb && \
        echo "${pkg}_$SLURM_VERSION-1_amd64.deb successfully downloaded" || \
        { echo "Failed to download ${pkg}_$SLURM_VERSION-1_amd64.deb"; exit 1; }; \
    done

RUN apt install -y /tmp/*.deb && rm -rf /tmp/*.deb

# Install slurm сhroot plugin
COPY common/chroot-plugin/chroot.c /usr/src/chroot-plugin/
COPY common/scripts/install_chroot_plugin.sh /opt/bin/
RUN chmod +x /opt/bin/install_chroot_plugin.sh && \
    /opt/bin/install_chroot_plugin.sh && \
    rm /opt/bin/install_chroot_plugin.sh

# Install parallel because it's required for enroot operation and used in the benchmark script
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
COPY common/enroot/enroot.conf /etc/enroot/
RUN chown 0:0 /etc/enroot/enroot.conf && chmod 644 /etc/enroot/enroot.conf

# Create node-local directories for enroot runtime data
RUN mkdir -p -m 777 /usr/share/enroot/enroot-data && \
    mkdir -p -m 755 /run/enroot

# Install slurm pyxis plugin
COPY common/scripts/install_pyxis_plugin.sh /opt/bin/
RUN chmod +x /opt/bin/install_pyxis_plugin.sh && \
    /opt/bin/install_pyxis_plugin.sh && \
    rm /opt/bin/install_pyxis_plugin.sh

# Install munge
COPY common/scripts/install_munge.sh /opt/bin/
RUN chmod +x /opt/bin/install_munge.sh && \
    /opt/bin/install_munge.sh && \
    rm /opt/bin/install_munge.sh

# We run munge in the same container so we need to create the /run/munge directory
RUN mkdir -m 755 /run/munge

# Copy srun_perf script that schedules jobs with GPU benchmark
COPY nccl_benchmark/scripts/srun_perf.sh /usr/bin/srun_perf.sh
RUN chmod +x /usr/bin/srun_perf.sh

# Update linker cache
RUN ldconfig

# Delete users & home because they will be linked from jail
RUN rm /etc/passwd* /etc/group* /etc/shadow* /etc/gshadow*
RUN rm -rf /home

ENV MUNGE_NUM_THREADS=10
ENV MUNGE_KEY_FILE=/etc/munge/munge.key
ENV MUNGE_PID_FILE=/run/munge/munged.pid
ENV MUNGE_SOCKET_FILE=/run/munge/munge.socket.2

# Copy & run the entrypoint script
COPY nccl_benchmark/nccl_benchmark_entrypoint.sh /opt/bin/nccl_benchmark_entrypoint.sh
RUN chmod +x /opt/bin/nccl_benchmark_entrypoint.sh
ENTRYPOINT ["/opt/bin/nccl_benchmark_entrypoint.sh"]
