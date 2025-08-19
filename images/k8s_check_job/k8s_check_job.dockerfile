ARG BASE_IMAGE=cr.eu-north1.nebius.cloud/soperator/ubuntu:noble

FROM $BASE_IMAGE AS k8s_check_job

RUN apt-get update && \
    apt-get install -y \
      openssh-client \
      retry && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*
