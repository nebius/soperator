FROM ubuntu:focal as login_sshd

ARG DEBIAN_FRONTEND=noninteractive

# TODO: Install only those dependencies that are required for running sshd + useful utilities
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
        iputils-ping \
        dnsutils \
        telnet \
        strace \
        vim \
        tree \
        lsof

# Install OpenSSH server
RUN apt install -y openssh-server

# Create root .ssh directory
RUN mkdir -m 700 -p /root/.ssh

# Copy script for complementing jail filesystem in runtime
COPY docker/common/scripts/complement_jail.sh /opt/bin/slurm/
RUN chmod +x /opt/bin/slurm/complement_jail.sh

# Update linker cache
RUN ldconfig

# Delete users & home because they will be linked from jail
RUN rm /etc/passwd* /etc/group* /etc/shadow* /etc/gshadow*
RUN rm -rf /home

# Expose the port used for accessing sshd
EXPOSE 22

# Copy & run the entrypoint script
COPY docker/login/sshd_entrypoint.sh /opt/bin/slurm/
RUN chmod +x /opt/bin/slurm/sshd_entrypoint.sh
ENTRYPOINT /opt/bin/slurm/sshd_entrypoint.sh
