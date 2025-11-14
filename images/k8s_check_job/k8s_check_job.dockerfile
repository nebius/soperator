# syntax=docker.io/docker/dockerfile-upstream:1.20.0
FROM cr.eu-north1.nebius.cloud/soperator/ubuntu:noble AS k8s_check_job

ARG DEBIAN_FRONTEND=noninteractive

# Install common packages and minimal python packages for Ansible
RUN apt-get update && \
    apt-get install -y \
        apt-transport-https \
        ca-certificates  \
        curl  \
        gnupg \
        python3.12="3.12.3-1ubuntu0.8" \
        python3.12-venv="3.12.3-1ubuntu0.8" \
        openssh-client \
        retry && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

COPY images/common/scripts/install_kubectl.sh /opt/bin/
RUN chmod +x /opt/bin/install_kubectl.sh && \
    /opt/bin/install_kubectl.sh && \
    rm /opt/bin/install_kubectl.sh

# Install Ansible and base configs
COPY ansible/ansible.cfg ansible/requirements.txt /opt/ansible/
RUN cd /opt/ansible && ln -sf /usr/bin/python3.12 /usr/bin/python3 && \
    python3 -m venv .venv && . .venv/bin/activate && pip install -r requirements.txt

ENV PATH="/opt/ansible/.venv/bin:${PATH}"
WORKDIR /opt/ansible

# Install python
COPY ansible/python.yml /opt/ansible/python.yml
COPY ansible/roles/python /opt/ansible/roles/python
RUN ansible-playbook -i localhost -c local python.yml -t python

# Copy role for Nebius health-check library
COPY ansible/nc-health-checker.yml /opt/ansible/nc-health-checker.yml
COPY ansible/roles/nc-health-checker /opt/ansible/roles/nc-health-checker

# Copy & run the entrypoint script
COPY images/k8s_check_job/k8s_check_job_entrypoint.sh /opt/bin/
RUN chmod +x /opt/bin/k8s_check_job_entrypoint.sh

ENTRYPOINT ["/opt/bin/k8s_check_job_entrypoint.sh"]
