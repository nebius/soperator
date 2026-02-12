# syntax=docker.io/docker/dockerfile-upstream:1.20.0

ARG CUDA_VERSION
ARG SLURM_VERSION
# https://github.com/nebius/ml-containers/pull/62
FROM cr.eu-north1.nebius.cloud/ml-containers/slurm_training_diag:slurm${SLURM_VERSION}-cuda${CUDA_VERSION}-ubuntu24.04-20260210085501 AS jail

# Create directory for pivoting host's root
RUN mkdir -m 555 /mnt/host

# Copy initial users
RUN rm /etc/passwd* /etc/group* /etc/shadow* /etc/gshadow*
COPY images/jail/init-users/* /etc/
RUN chmod 644 /etc/passwd /etc/group && chown 0:0 /etc/passwd /etc/group && \
    chmod 640 /etc/shadow /etc/gshadow && chown 0:42 /etc/shadow /etc/gshadow && \
    chmod 440 /etc/sudoers && chown 0:0 /etc/sudoers

# Install useful packages
RUN apt update && \
    apt install -y \
        bc \
        gettext-base \
        git \
        less \
        netcat-openbsd \
        strace \
        pciutils \
        rsync \
        htop \
        hwloc \
        bsdmainutils \
        tmux \
        aptitude && \
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

# Install nvtop GPU monitoring utility
COPY ansible/nvtop.yml /opt/ansible/nvtop.yml
COPY ansible/roles/nvtop /opt/ansible/roles/nvtop
RUN cd /opt/ansible && \
    ansible-playbook -i inventory/ -c local nvtop.yml

## Install Docker CLI
COPY ansible/docker-cli.yml /opt/ansible/docker-cli.yml
COPY ansible/roles/docker-cli /opt/ansible/roles/docker-cli
RUN cd /opt/ansible && \
    ansible-playbook -i inventory/ -c local docker-cli.yml

## Install GDRCopy libraries & executables
COPY ansible/gdrcopy.yml /opt/ansible/gdrcopy.yml
COPY ansible/roles/gdrcopy /opt/ansible/roles/gdrcopy
RUN cd /opt/ansible && \
    ansible-playbook -i inventory/ -c local gdrcopy.yml

## Install nvidia-container-toolkit (for enroot usage)
COPY ansible/nvidia-container-toolkit.yml /opt/ansible/nvidia-container-toolkit.yml
COPY ansible/roles/nvidia-container-toolkit /opt/ansible/roles/nvidia-container-toolkit
RUN cd /opt/ansible && \
    ansible-playbook -i inventory/ -c local nvidia-container-toolkit.yml -t nvidia-container-toolkit

# Setup the default $HOME directory content
COPY ansible/skel.yml /opt/ansible/skel.yml
COPY ansible/roles/skel /opt/ansible/roles/skel
RUN cd /opt/ansible && \
    ansible-playbook -i inventory/ -c local skel.yml

# Replace SSH "message of the day" scripts
COPY ansible/motd.yml /opt/ansible/motd.yml
COPY ansible/roles/motd /opt/ansible/roles/motd
RUN cd /opt/ansible && \
    ansible-playbook -i inventory/ -c local motd.yml

# Copy wrapper scripts and utilities
COPY ansible/soperator-scripts.yml /opt/ansible/soperator-scripts.yml
COPY ansible/roles/soperator-scripts /opt/ansible/roles/soperator-scripts
RUN cd /opt/ansible && \
    ansible-playbook -i inventory/ -c local soperator-scripts.yml

# Install Nebius health-check library
COPY ansible/nc-health-checker.yml /opt/ansible/nc-health-checker.yml
COPY ansible/roles/nc-health-checker /opt/ansible/roles/nc-health-checker
RUN cd /opt/ansible && \
    ansible-playbook -i inventory/ -c local nc-health-checker.yml

# Remove ansible
RUN rm -rf /opt/ansible

# Save the initial jail version to a file
COPY VERSION /etc/soperator-jail-version

# Update linker cache
RUN ldconfig

#######################################################################################################################
FROM restic/restic:0.18.0 AS untaped

COPY --from=jail / /jail

RUN restic init --insecure-no-password --repo /jail_restic && \
    cd /jail && \
    restic --insecure-no-password --repo /jail_restic backup ./ \
        --no-scan --no-cache --read-concurrency 16 \
        --compression max --pack-size 8 \
        --host soperator

#######################################################################################################################
FROM restic/restic:0.18.0 AS populate_jail

COPY --from=untaped /jail_restic /jail_restic

COPY images/jail/populate_jail_entrypoint.sh /opt/bin/
RUN chmod +x /opt/bin/populate_jail_entrypoint.sh
ENTRYPOINT ["/opt/bin/populate_jail_entrypoint.sh"]

