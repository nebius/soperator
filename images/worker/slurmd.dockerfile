# syntax=docker.io/docker/dockerfile-upstream:1.20.0

ARG SLURM_VERSION

# https://github.com/nebius/ml-containers/pull/98
FROM cr.eu-north1.nebius.cloud/ml-containers/slurm:${SLURM_VERSION}-20260819104842 AS worker_slurmd

# Install useful packages
RUN apt-get update && \
    apt -y install \
        pciutils \
        iproute2 \
        kmod \
        libncurses5-dev \
        supervisor \
        openssh-server \
        nginx-extras \
        libnginx-mod-http-js && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Create dummy library for replacing GPU-specific libraries on CPU workers in GPU clusters
RUN ALT_ARCH="$(uname -m)" && \
    mkdir -p /usr/src/dummy && \
    cd /usr/src/dummy && \
    echo 'int main() { return 0; }' > dummy.c && \
    gcc -shared -o libdummy.so dummy.c && \
    mkdir -p "/lib/${ALT_ARCH}-linux-gnu" && \
    cp libdummy.so "/lib/${ALT_ARCH}-linux-gnu/"

COPY ansible/sssd.yml /opt/ansible/sssd.yml
COPY ansible/roles/sssd /opt/ansible/roles/sssd
RUN cd /opt/ansible && \
    ansible-playbook -i inventory/ -c local sssd.yml

# Install slurm сhroot plugin
COPY images/common/chroot-plugin/chroot.c /usr/src/chroot-plugin/
COPY images/common/scripts/install_chroot_plugin.sh /opt/bin/
RUN chmod +x /opt/bin/install_chroot_plugin.sh && \
    /opt/bin/install_chroot_plugin.sh && \
    rm /opt/bin/install_chroot_plugin.sh

# Install NCCL Debug SPANK plugin
COPY images/common/spank-nccl-debug/src /usr/src/soperator/spank/nccld-debug
COPY images/common/scripts/install_nccld_debug_plugin.sh /opt/bin/
RUN chmod +x /opt/bin/install_nccld_debug_plugin.sh && \
    /opt/bin/install_nccld_debug_plugin.sh && \
    rm /opt/bin/install_nccld_debug_plugin.sh

# Install NCCL Inspector PreConf SPANK plugin
COPY ansible/spank_nccl_inspector_preconf.yml /opt/ansible/spank_nccl_inspector_preconf.yml
COPY ansible/roles/spank_nccl_inspector_preconf /opt/ansible/roles/spank_nccl_inspector_preconf
RUN cd /opt/ansible && \
    ansible-playbook -i inventory/ -c local \
      -e spank_nccl_inspector_preconf_dump_dir_create=false \
      spank_nccl_inspector_preconf.yml

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
ARG SLURM_DEB_VERSION
ARG PYXIS_VERSION=0.24.0
RUN apt-get update && \
    apt -y install nvslurm-plugin-pyxis=${SLURM_DEB_VERSION}-${PYXIS_VERSION}-1 && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

## Install nvidia-container-toolkit (for enroot usage)
COPY ansible/nvidia_container_toolkit.yml /opt/ansible/nvidia_container_toolkit.yml
COPY ansible/roles/nvidia_container_toolkit /opt/ansible/roles/nvidia_container_toolkit
RUN cd /opt/ansible && \
    ansible-playbook -i inventory/ -c local nvidia_container_toolkit.yml -t nvidia_container_toolkit

# Install Docker
RUN apt-get update && \
    apt install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin && \
    apt clean

# Copy Docker daemon config
COPY images/worker/docker/daemon.json /etc/docker/daemon.json
COPY images/worker/nginx/soperator-docker-proxy.conf /etc/nginx/soperator-docker-proxy.conf
COPY images/worker/nginx/docker_proxy.js /etc/nginx/njs/docker_proxy.js

# Copy script for complementing jail filesystem in runtime
COPY images/common/scripts/complement_jail.sh /opt/bin/slurm/

# Copy script for bind-mounting slurm into the jail
COPY images/common/scripts/bind_slurm_common.sh /opt/bin/slurm/

# Copy scripts for rebooting K8s nodes and handing off worker operations
COPY images/common/scripts/reboot.sh /opt/bin/slurm/
COPY images/common/scripts/worker_handoff.py /opt/bin/slurm/

RUN chmod +x /opt/bin/slurm/complement_jail.sh && \
    chmod +x /opt/bin/slurm/bind_slurm_common.sh && \
    chmod +x /opt/bin/slurm/reboot.sh && \
    chmod +x /opt/bin/slurm/worker_handoff.py

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
COPY images/worker/write_soperator_metadata.sh /opt/bin/slurm/

# Copy worker init script (controller readiness + topology for ephemeral nodes)
COPY images/worker/worker_init.py /opt/bin/slurm/

# Copy supervisord entrypoint script
COPY images/worker/supervisord_entrypoint.sh /opt/bin/slurm/
COPY images/worker/docker_proxy_nginx_entrypoint.sh /opt/bin/slurm/
COPY images/worker/dockerd_entrypoint.sh /opt/bin/slurm/

RUN chmod +x /opt/bin/slurm/slurmd_entrypoint.sh && \
    chmod +x /opt/bin/slurm/write_soperator_metadata.sh && \
    chmod +x /opt/bin/slurm/supervisord_entrypoint.sh && \
    chmod +x /opt/bin/slurm/worker_init.py && \
    chmod +x /opt/bin/slurm/docker_proxy_nginx_entrypoint.sh && \
    chmod +x /opt/bin/slurm/dockerd_entrypoint.sh

# Start supervisord that manages both slurmd and sshd as child processes
ENTRYPOINT ["/opt/bin/slurm/supervisord_entrypoint.sh"]
