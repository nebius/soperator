# syntax=docker.io/docker/dockerfile-upstream:1.20.0

ARG SLURM_VERSION

# https://github.com/nebius/ml-containers/pull/43
FROM cr.eu-north1.nebius.cloud/ml-containers/slurm:${SLURM_VERSION}-20260114082803 AS controller_slurmdbd

# Expose the port used for accessing slurmdbd
EXPOSE 6819

# Create dir and file for multilog hack
RUN mkdir -p /var/log/slurm/multilog && \
    touch /var/log/slurm/multilog/current && \
    ln -s /var/log/slurm/multilog/current /var/log/slurm/slurmdbd.log

# Copy & run the entrypoint script
COPY images/accounting/slurmdbd_entrypoint.sh /opt/bin/slurm/
RUN chmod +x /opt/bin/slurm/slurmdbd_entrypoint.sh
ENTRYPOINT ["/opt/bin/slurm/slurmdbd_entrypoint.sh"]
