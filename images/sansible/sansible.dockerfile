# syntax=docker.io/docker/dockerfile-upstream:1.20.0

# https://github.com/nebius/ml-containers/pull/52
FROM cr.eu-north1.nebius.cloud/ml-containers/ansible_roles:noble-20260129115242 AS sansible

# Install common packages
RUN apt update && \
    apt install -y openssh-client && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Copy all Ansible playbooks
COPY ansible/ /opt/ansible/
WORKDIR /opt/ansible/

# Copy the entrypoint script
COPY images/sansible/sansible_entrypoint.sh /opt/bin/
RUN chmod +x /opt/bin/sansible_entrypoint.sh

ENTRYPOINT ["/opt/bin/sansible_entrypoint.sh"]
