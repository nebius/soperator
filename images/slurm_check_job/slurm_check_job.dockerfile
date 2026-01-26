# syntax=docker.io/docker/dockerfile-upstream:1.20.0

ARG SLURM_VERSION

# https://github.com/nebius/ml-containers/pull/43
FROM cr.eu-north1.nebius.cloud/ml-containers/slurm:${SLURM_VERSION}-20260123155557 AS slurm_check_job

# Install slurm сhroot plugin
COPY images/common/chroot-plugin/chroot.c /usr/src/chroot-plugin/
COPY images/common/scripts/install_chroot_plugin.sh /opt/bin/
RUN chmod +x /opt/bin/install_chroot_plugin.sh && \
    /opt/bin/install_chroot_plugin.sh && \
    rm /opt/bin/install_chroot_plugin.sh

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

ARG SLURM_VERSION
ARG PYXIS_VERSION=0.21.0
# Install slurm pyxis plugin \
RUN apt-get update && \
    apt -y install nvslurm-plugin-pyxis=${SLURM_VERSION}-${PYXIS_VERSION}-1 && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Install NCCL debug plugin
COPY images/common/spank-nccl-debug/src /usr/src/soperator/spank/nccld-debug
COPY images/common/scripts/install_nccld_debug_plugin.sh /opt/bin/
RUN chmod +x /opt/bin/install_nccld_debug_plugin.sh && \
    /opt/bin/install_nccld_debug_plugin.sh && \
    rm /opt/bin/install_nccld_debug_plugin.sh

# Install kubectl
RUN ARCH="$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/')" && \
    KUBECTL_VERSION="$(curl -Ls https://dl.k8s.io/release/stable.txt)" && \
    echo "Downloading kubectl from https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${ARCH}/kubectl" && \
    curl -LO "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${ARCH}/kubectl" && \
    install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl && \
    rm kubectl

# Update linker cache
RUN ldconfig

# Create symlinks for SPANK plugins so Slurm can find them at /usr/lib/slurm/
# Plugins are installed at /usr/lib/${ARCH}-linux-gnu/slurm/ but Slurm looks at /usr/lib/slurm/
RUN ARCH="$(uname -m)" && \
    mkdir -p /usr/lib/slurm && \
    ln -s "/usr/lib/${ARCH}-linux-gnu/slurm/chroot.so" /usr/lib/slurm/chroot.so && \
    ln -s "/usr/lib/${ARCH}-linux-gnu/slurm/spank_pyxis.so" /usr/lib/slurm/spank_pyxis.so && \
    ln -s "/usr/lib/${ARCH}-linux-gnu/slurm/spanknccldebug.so" /usr/lib/slurm/spanknccldebug.so

# Disable NCCL debug plugin by default for slurm jobs
ENV SNCCLD_ENABLED="false"

# Delete users & home because they will be linked from jail
RUN rm /etc/passwd* /etc/group* /etc/shadow* /etc/gshadow*
RUN rm -rf /home

# Copy & run the entrypoint script
COPY images/slurm_check_job/slurm_check_job_entrypoint.sh /opt/bin/slurm/
COPY images/slurm_check_job/slurm_submit_jobs.sh /opt/bin/slurm/
RUN chmod +x /opt/bin/slurm/slurm_check_job_entrypoint.sh \
    && chmod +x /opt/bin/slurm/slurm_submit_jobs.sh

ENTRYPOINT ["/opt/bin/slurm/slurm_check_job_entrypoint.sh"]
