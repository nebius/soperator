# syntax=docker.io/docker/dockerfile-upstream:1.20.0

# https://github.com/nebius/ml-containers/pull/104
FROM cr.nebius.cloud/ml-containers/neubuntu:noble-20260916135615 AS k8s_check_job

# Install common packages
RUN apt update && \
    apt install -y \
        openssh-client \
        retry && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Install kubectl
ARG KUBECTL_VERSION=v1.36.4
RUN ARCH="$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/')" && \
    echo "Downloading kubectl from https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${ARCH}/kubectl" && \
    curl -fsSLO "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${ARCH}/kubectl" && \
    install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl && \
    rm kubectl

# Copy the entrypoint script
COPY images/k8s_check_job/k8s_check_job_entrypoint.sh /opt/bin/
RUN chmod +x /opt/bin/k8s_check_job_entrypoint.sh

ENTRYPOINT ["/opt/bin/k8s_check_job_entrypoint.sh"]
