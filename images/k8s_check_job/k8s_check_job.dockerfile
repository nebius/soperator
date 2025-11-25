# syntax=docker.io/docker/dockerfile-upstream:1.20.0

ARG BASE_IMAGE=cr.eu-north1.nebius.cloud/soperator/ubuntu:noble

FROM $BASE_IMAGE AS k8s_check_job

RUN apt-get update && \
    apt-get install -y \
      openssh-client \
      retry && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

COPY images/common/scripts/install_kubectl.sh /opt/bin/
RUN chmod +x /opt/bin/install_kubectl.sh && \
    /opt/bin/install_kubectl.sh && \
    rm /opt/bin/install_kubectl.sh

# Install python
RUN apt-get update && \
    apt-get install -y \
        python3.12="3.12.3-1ubuntu0.9" \
        python3.12-dev="3.12.3-1ubuntu0.9" \
        python3.12-venv="3.12.3-1ubuntu0.9" \
        python3-pip="24.0+dfsg-1ubuntu1.3" \
        python3-pip-whl="24.0+dfsg-1ubuntu1.3" && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/* && \
    ln -sf /usr/bin/python3.12 /usr/bin/python && \
    ln -sf /usr/bin/python3.12 /usr/bin/python3
