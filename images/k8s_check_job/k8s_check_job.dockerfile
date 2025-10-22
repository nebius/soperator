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

COPY images/common/scripts/install_python.sh /opt/bin/
RUN chmod +x /opt/bin/install_python.sh && \
    /opt/bin/install_python.sh && \
    rm /opt/bin/install_python.sh
