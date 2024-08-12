ARG BASE_IMAGE=ubuntu:focal

FROM $BASE_IMAGE AS nccl_benchmark

ARG SLURM_VERSION=23.11.6

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
        vim

# TODO: Install only necessary packages
# Copy and install Slurm packages
COPY --from=slurm /usr/src/slurm-smd-client_$SLURM_VERSION-1_amd64.deb /tmp/
COPY --from=slurm /usr/src/slurm-smd-dev_$SLURM_VERSION-1_amd64.deb /tmp/
COPY --from=slurm /usr/src/slurm-smd-libnss-slurm_$SLURM_VERSION-1_amd64.deb /tmp/
COPY --from=slurm /usr/src/slurm-smd-libpmi0_$SLURM_VERSION-1_amd64.deb /tmp/
COPY --from=slurm /usr/src/slurm-smd-libpmi2-0_$SLURM_VERSION-1_amd64.deb /tmp/
COPY --from=slurm /usr/src/slurm-smd-libslurm-perl_$SLURM_VERSION-1_amd64.deb /tmp/
COPY --from=slurm /usr/src/slurm-smd-openlava_$SLURM_VERSION-1_all.deb /tmp/
COPY --from=slurm /usr/src/slurm-smd_$SLURM_VERSION-1_amd64.deb /tmp/
RUN apt install -y /tmp/*.deb && rm -rf /tmp/*.deb

# Install slurm plugins
COPY docker/common/chroot-plugin/chroot.c /usr/src/chroot-plugin/
COPY docker/common/scripts/install_slurm_plugins.sh /opt/bin/
RUN chmod +x /opt/bin/install_slurm_plugins.sh && \
    /opt/bin/install_slurm_plugins.sh && \
    rm /opt/bin/install_slurm_plugins.sh

# Install munge
COPY docker/common/scripts/install_munge.sh /opt/bin/
RUN chmod +x /opt/bin/install_munge.sh && \
    /opt/bin/install_munge.sh && \
    rm /opt/bin/install_munge.sh

# We run munge in the same container so we need to create the /run/munge directory
RUN mkdir -m 755 /run/munge

# Install parallel because it's used in the benchmark script
COPY docker/common/scripts/install_parallel.sh /opt/bin/
RUN chmod +x /opt/bin/install_parallel.sh && \
    /opt/bin/install_parallel.sh && \
    rm /opt/bin/install_parallel.sh

# Copy srun_perf script that schedules jobs with GPU benchmark
COPY docker/nccl_benchmark/scripts/srun_perf.sh /usr/bin/srun_perf.sh
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
COPY docker/nccl_benchmark/nccl_benchmark_entrypoint.sh /opt/bin/nccl_benchmark_entrypoint.sh
RUN chmod +x /opt/bin/nccl_benchmark_entrypoint.sh
ENTRYPOINT /opt/bin/nccl_benchmark_entrypoint.sh
