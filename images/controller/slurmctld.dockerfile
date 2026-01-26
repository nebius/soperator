# syntax=docker.io/docker/dockerfile-upstream:1.20.0

ARG SLURM_VERSION

# https://github.com/nebius/ml-containers/pull/43
FROM cr.eu-north1.nebius.cloud/ml-containers/slurm:${SLURM_VERSION}-20260123155557 AS controller_slurmctld

# Delete users & home because they will be linked from jail
RUN rm /etc/passwd* /etc/group* /etc/shadow* /etc/gshadow*
RUN rm -rf /home

# Expose the port used for accessing slurmctld
EXPOSE 6817

# Create dir and file for multilog hack
RUN mkdir -p /var/log/slurm/multilog && \
    touch /var/log/slurm/multilog/current && \
    ln -s /var/log/slurm/multilog/current /var/log/slurm/slurmctld.log

# Copy & run the entrypoint script
COPY images/controller/slurmctld_entrypoint.sh /opt/bin/slurm/
RUN chmod +x /opt/bin/slurm/slurmctld_entrypoint.sh
ENTRYPOINT ["/opt/bin/slurm/slurmctld_entrypoint.sh"]
