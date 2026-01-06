# syntax=docker.io/docker/dockerfile-upstream:1.20.0

# https://github.com/nebius/ml-containers/blob/main/.github/workflows/neubuntu.yml
FROM cr.eu-north1.nebius.cloud/ml-containers/neubuntu:noble-20251224121141 AS k8s_check_job

ARG DEBIAN_FRONTEND=noninteractive

# Install common packages and minimal python packages for Ansible
RUN apt install --update -y \
        python3.12="3.12.3-1ubuntu0.9" \
        python3.12-venv="3.12.3-1ubuntu0.9" \
        openssh-client \
        retry && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

COPY images/common/scripts/install_kubectl.sh /opt/bin/
RUN chmod +x /opt/bin/install_kubectl.sh && \
    /opt/bin/install_kubectl.sh && \
    rm /opt/bin/install_kubectl.sh

# Install Ansible and copy playbooks
COPY ansible/ /opt/ansible/
RUN cd /opt/ansible && ln -sf /usr/bin/python3.12 /usr/bin/python3 && \
    python3 -m venv .venv && . .venv/bin/activate && pip install -r requirements.txt

ENV PATH="/opt/ansible/.venv/bin:${PATH}"
WORKDIR /opt/ansible

# Install python
RUN ansible-playbook -i inventory/ -c local python.yml

# Manage repositories
RUN ansible-playbook -i inventory/ -c local repos.yml

# Copy the entrypoint script
COPY images/k8s_check_job/k8s_check_job_entrypoint.sh /opt/bin/
RUN chmod +x /opt/bin/k8s_check_job_entrypoint.sh

ENTRYPOINT ["/opt/bin/k8s_check_job_entrypoint.sh"]
