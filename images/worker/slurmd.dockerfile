# syntax=docker.io/docker/dockerfile-upstream:1.20.0

ARG SLURM_VERSION

# https://github.com/nebius/ml-containers/pull/55
FROM cr.eu-north1.nebius.cloud/ml-containers/slurm:${SLURM_VERSION}-20260205130055 AS worker_slurmd

# Install useful packages
RUN apt-get update && \
    apt -y install \
        pciutils \
        iproute2 \
        infiniband-diags \
        kmod \
        libncurses5-dev \
        supervisor \
        openssh-server && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Install OpenMPI
ARG OPENMPI_VERSION=4.1.7a1
COPY images/common/scripts/install_openmpi.sh /opt/bin/
RUN chmod +x /opt/bin/install_openmpi.sh && \
    /opt/bin/install_openmpi.sh && \
    rm /opt/bin/install_openmpi.sh

RUN arch=$(uname -m) && \
    if [ "$arch" = "x86_64" ]; then alt_arch="x86_64"; \
    elif [ "$arch" = "aarch64" ]; then alt_arch="aarch64"; \
    else echo "Unsupported arch: $arch" && exit 1; fi && \
    echo "LD_LIBRARY_PATH=/usr/mpi/gcc/openmpi-${OPENMPI_VERSION}/lib:/lib/${alt_arch}-linux-gnu:/usr/lib/${alt_arch}-linux-gnu:/usr/local/cuda/targets/${alt_arch}-linux/lib" >> /etc/environment
ENV PATH=${PATH}:/usr/mpi/gcc/openmpi-${OPENMPI_VERSION}/bin

# Install slurm сhroot plugin
COPY images/common/chroot-plugin/chroot.c /usr/src/chroot-plugin/
COPY images/common/scripts/install_chroot_plugin.sh /opt/bin/
RUN chmod +x /opt/bin/install_chroot_plugin.sh && \
    /opt/bin/install_chroot_plugin.sh && \
    rm /opt/bin/install_chroot_plugin.sh

# Install NCCL debug plugin
COPY images/common/spank-nccl-debug/src /usr/src/soperator/spank/nccld-debug
COPY images/common/scripts/install_nccld_debug_plugin.sh /opt/bin/
RUN chmod +x /opt/bin/install_nccld_debug_plugin.sh && \
    /opt/bin/install_nccld_debug_plugin.sh && \
    rm /opt/bin/install_nccld_debug_plugin.sh

# Install enroot
COPY images/common/scripts/install_enroot.sh /opt/bin/
RUN chmod +x /opt/bin/install_enroot.sh && \
    /opt/bin/install_enroot.sh && \
    rm /opt/bin/install_enroot.sh

# Copy enroot configuration
COPY images/common/enroot/enroot.conf /etc/enroot/
COPY images/common/enroot/custom-dirs.conf /etc/enroot/enroot.conf.d/
RUN chown 0:0 /etc/enroot/enroot.conf && \
    chmod 644 /etc/enroot/enroot.conf && \
    chown 0:0 /etc/enroot/enroot.conf.d/custom-dirs.conf && \
    chmod 644 /etc/enroot/enroot.conf.d/custom-dirs.conf

# Install slurm pyxis plugin
ARG SLURM_VERSION
ARG PYXIS_VERSION=0.21.0
RUN apt-get update && \
    apt -y install nvslurm-plugin-pyxis=${SLURM_VERSION}-${PYXIS_VERSION}-1 && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Install nvidia-container-toolkit
COPY images/common/scripts/install_container_toolkit.sh /opt/bin/
RUN chmod +x /opt/bin/install_container_toolkit.sh && \
    /opt/bin/install_container_toolkit.sh && \
    rm /opt/bin/install_container_toolkit.sh

# Copy NVIDIA Container Toolkit config
COPY ansible/roles/nvidia-container-toolkit/files/config.toml /etc/nvidia-container-runtime/config.toml

# Install Docker
RUN apt-get update && \
    apt install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin && \
    apt clean

# Copy Docker daemon config
COPY images/worker/docker/daemon.json /etc/docker/daemon.json

# Copy script for complementing jail filesystem in runtime
COPY images/common/scripts/complement_jail.sh /opt/bin/slurm/

# Copy script for bind-mounting slurm into the jail
COPY images/common/scripts/bind_slurm_common.sh /opt/bin/slurm/

# Copy script for rebooting K8s nodes
COPY images/common/scripts/reboot.sh /opt/bin/slurm/

RUN chmod +x /opt/bin/slurm/complement_jail.sh && \
    chmod +x /opt/bin/slurm/bind_slurm_common.sh && \
    chmod +x /opt/bin/slurm/reboot.sh

# Create single folder with slurm plugins for all architectures
RUN mkdir -p /usr/lib/slurm && \
    for dir in /usr/lib/*-linux-gnu/slurm; do \
      [ -d "$dir" ] && ln -sf $dir/* /usr/lib/slurm/ 2>/dev/null || true; \
    done

# Update linker cache
RUN ldconfig

# Delete users & home because they will be linked from jail
RUN rm /etc/passwd* /etc/group* /etc/shadow* /etc/gshadow*
RUN rm -rf /home

# Delete SSH "message of the day" scripts because they aren't needed on worker nodes
RUN rm -rf /etc/update-motd.d

# Expose the port used for accessing slurmd
EXPOSE 6818

# Create dir and file for multilog hack
RUN mkdir -p /var/log/slurm/multilog && \
    touch /var/log/slurm/multilog/current && \
    ln -s /var/log/slurm/multilog/current /var/log/slurm/slurmd.log

# Copy slurmd entrypoint script
COPY images/worker/slurmd_entrypoint.sh /opt/bin/slurm/

# Copy supervisord entrypoint script
COPY images/worker/supervisord_entrypoint.sh /opt/bin/slurm/

# Copy wait-for-controller script
COPY images/worker/wait-for-controller.sh /opt/bin/slurm/

RUN chmod +x /opt/bin/slurm/slurmd_entrypoint.sh && \
    chmod +x /opt/bin/slurm/supervisord_entrypoint.sh && \
    chmod +x /opt/bin/slurm/wait-for-controller.sh

# Start supervisord that manages both slurmd and sshd as child processes
ENTRYPOINT ["/opt/bin/slurm/supervisord_entrypoint.sh"]
