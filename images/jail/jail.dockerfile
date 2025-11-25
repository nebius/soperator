# syntax=docker.io/docker/dockerfile-upstream:1.20.0

FROM cr.eu-north1.nebius.cloud/soperator/cuda_base:13.0.2-ubuntu24.04-nccl2.28.7-1-14542c2 AS cuda

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

# Create directory for pivoting host's root
RUN mkdir -m 555 /mnt/host

# Copy initial users
RUN rm /etc/passwd* /etc/group* /etc/shadow* /etc/gshadow*
COPY images/jail/init-users/* /etc/
RUN chmod 644 /etc/passwd /etc/group && chown 0:0 /etc/passwd /etc/group && \
    chmod 640 /etc/shadow /etc/gshadow && chown 0:42 /etc/shadow /etc/gshadow && \
    chmod 440 /etc/sudoers && chown 0:0 /etc/sudoers

# Install minimal python packages for Ansible
RUN apt-get update && \
    apt-get install -y \
        python3.12="3.12.3-1ubuntu0.9" \
        python3.12-venv="3.12.3-1ubuntu0.9"

# Install Ansible and base configs
COPY ansible/ansible.cfg ansible/requirements.txt ansible/run.yml /opt/ansible/
COPY ansible/inventory/jail/hosts.ini /opt/ansible/inventory/jail/hosts.ini
RUN cd /opt/ansible && ln -sf /usr/bin/python3.12 /usr/bin/python3 && \
    python3 -m venv .venv && . .venv/bin/activate && pip install -r requirements.txt

ENV PATH="/opt/ansible/.venv/bin:${PATH}"
WORKDIR /opt/ansible

# Install python
COPY ansible/python.yml /opt/ansible/python.yml
COPY ansible/roles/python /opt/ansible/roles/python
RUN ansible-playbook -i localhost -c local python.yml -t python

# Install common packages
COPY ansible/common-packages.yml /opt/ansible/common-packages.yml
COPY ansible/roles/common-packages /opt/ansible/roles/common-packages
RUN ansible-playbook -i localhost -c local common-packages.yml -t common-packages

# Install useful packages
RUN apt update && \
    apt install -y \
        bc \
        flex \
        gettext-base \
        git \
        less \
        lsof \
        iputils-ping \
        dnsutils \
        telnet \
        netcat-openbsd \
        strace \
        tree \
        vim \
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
RUN ansible-playbook -i localhost -c local nvtop.yml -t nvtop

## Install Docker CLI
COPY ansible/docker-cli.yml /opt/ansible/docker-cli.yml
COPY ansible/roles/docker-cli /opt/ansible/roles/docker-cli
RUN ansible-playbook -i localhost -c local docker-cli.yml -t docker-cli

# Install OpenMPI
COPY ansible/openmpi.yml /opt/ansible/openmpi.yml
COPY ansible/roles/openmpi /opt/ansible/roles/openmpi
RUN ansible-playbook -i localhost -c local openmpi.yml -t openmpi

# Install dcgmi tools
# https://docs.nvidia.com/datacenter/dcgm/latest/user-guide/dcgm-diagnostics.html
COPY ansible/dcgmi.yml /opt/ansible/dcgmi.yml
COPY ansible/roles/dcgmi /opt/ansible/roles/dcgmi
RUN ansible-playbook -i localhost -c local dcgmi.yml -t dcgmi

## Install GDRCopy libraries & executables
COPY ansible/gdrcopy.yml /opt/ansible/gdrcopy.yml
COPY ansible/roles/gdrcopy /opt/ansible/roles/gdrcopy
RUN ansible-playbook -i localhost -c local gdrcopy.yml -t gdrcopy

## Install nvidia-container-toolkit (for enroot usage)
COPY ansible/nvidia-container-toolkit.yml /opt/ansible/nvidia-container-toolkit.yml
COPY ansible/roles/nvidia-container-toolkit /opt/ansible/roles/nvidia-container-toolkit
RUN ansible-playbook -i localhost -c local nvidia-container-toolkit.yml -t nvidia-container-toolkit

# Setup the default $HOME directory content
COPY ansible/skel.yml /opt/ansible/skel.yml
COPY ansible/roles/skel /opt/ansible/roles/skel
RUN ansible-playbook -i localhost -c local skel.yml -t skel

# Replace SSH "message of the day" scripts
COPY ansible/motd.yml /opt/ansible/motd.yml
COPY ansible/roles/motd /opt/ansible/roles/motd
RUN ansible-playbook -i localhost -c local motd.yml -t motd

# Copy wrapper scripts and utilities
COPY ansible/soperator-scripts.yml /opt/ansible/soperator-scripts.yml
COPY ansible/roles/soperator-scripts /opt/ansible/roles/soperator-scripts
RUN ansible-playbook -i localhost -c local soperator-scripts.yml -t wrappers

# Install slurm client and divert files
COPY ansible/slurm-install.yml /opt/ansible/slurm-install.yml
COPY ansible/roles/slurm-client /opt/ansible/roles/slurm-client
COPY ansible/roles/slurm-divert /opt/ansible/roles/slurm-divert
RUN ansible-playbook -i localhost -c local slurm-install.yml -t slurm-install

# Install Nebius health-check library
COPY ansible/nc-health-checker.yml /opt/ansible/nc-health-checker.yml
COPY ansible/roles/nc-health-checker /opt/ansible/roles/nc-health-checker
RUN ansible-playbook -i localhost -c local nc-health-checker.yml -t nc-health-checker

# Remove ansible
RUN rm -rf /opt/ansible

# Save the initial jail version to a file
COPY VERSION /etc/soperator-jail-version

# Update linker cache
RUN ldconfig
