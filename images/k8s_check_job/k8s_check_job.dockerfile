ARG BASE_IMAGE=ubuntu:jammy

FROM $BASE_IMAGE AS k8s_check_job

RUN apt-get update && \
    apt-get install -y openssh-client && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*
