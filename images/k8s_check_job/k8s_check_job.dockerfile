# syntax=docker.io/docker/dockerfile-upstream:1.20.0

# https://github.com/nebius/ml-containers/pull/56
FROM cr.eu-north1.nebius.cloud/ml-containers/neubuntu:noble-20260206131521 AS k8s_check_job

# Install common packages
RUN apt update && \
    apt install -y \
        openssh-client \
        retry && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Install kubectl
RUN ARCH="$(uname -m | sed 's/x86_64/amd64/; s/aarch64/arm64/')" && \
    KUBECTL_VERSION="$(curl -Ls https://dl.k8s.io/release/stable.txt)" && \
    echo "Downloading kubectl from https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${ARCH}/kubectl" && \
    curl -LO "https://dl.k8s.io/release/${KUBECTL_VERSION}/bin/linux/${ARCH}/kubectl" && \
    install -o root -g root -m 0755 kubectl /usr/local/bin/kubectl && \
    rm kubectl

# Copy the entrypoint script
COPY images/k8s_check_job/k8s_check_job_entrypoint.sh /opt/bin/
RUN chmod +x /opt/bin/k8s_check_job_entrypoint.sh

ENTRYPOINT ["/opt/bin/k8s_check_job_entrypoint.sh"]
