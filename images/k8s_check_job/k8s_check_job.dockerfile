# syntax=docker.io/docker/dockerfile-upstream:1.20.0
FROM cr.eu-north1.nebius.cloud/soperator/ubuntu:noble AS k8s_check_job

ARG DEBIAN_FRONTEND=noninteractive

# Install certificates (required for using snapshots)
RUN apt install -y --update ca-certificates=20240203

# Install common packages and minimal python packages for Ansible
RUN apt install --update --snapshot 20251126T093556Z -y \
        apt-transport-https \
        curl  \
        gnupg \
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
