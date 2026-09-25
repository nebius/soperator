# syntax=docker.io/docker/dockerfile-upstream:1.20.0

ARG SLURM_VERSION

# https://github.com/nebius/ml-containers/pull/107
FROM cr.nebius.cloud/ml-containers/slurm:${SLURM_VERSION}-20260925081218 AS slurmrestd

# Expose the port used for accessing slurmrestd
EXPOSE 6820

# Copy & run the entrypoint script
COPY images/restd/slurmrestd_entrypoint.sh /opt/bin/slurm/
RUN chmod +x /opt/bin/slurm/slurmrestd_entrypoint.sh
ENTRYPOINT ["/opt/bin/slurm/slurmrestd_entrypoint.sh"]
