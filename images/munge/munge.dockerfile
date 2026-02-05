# syntax=docker.io/docker/dockerfile-upstream:1.20.0

# https://github.com/nebius/ml-containers/pull/55
FROM cr.eu-north1.nebius.cloud/ml-containers/neubuntu:noble-20260205123821 AS munge

RUN apt-get update && \
    apt -y install \
        munge \
        libmunge-dev && \
    apt-get clean && \
    rm -rf /var/lib/apt/lists/*

# Fix permissions \
RUN chmod -R 700 /etc/munge /var/log/munge && \
    chmod -R 711 /var/lib/munge && \
    chown -R 0:0 /etc/munge /var/log/munge /var/lib/munge

# Update linker cache
RUN ldconfig

ENV MUNGE_NUM_THREADS=10
ENV MUNGE_KEY_FILE=/etc/munge/munge.key
ENV MUNGE_PID_FILE=/run/munge/munged.pid
ENV MUNGE_SOCKET_FILE=/run/munge/munge.socket.2

# Copy & run the entrypoint script
COPY images/munge/munge_entrypoint.sh /opt/bin/
RUN chmod +x /opt/bin/munge_entrypoint.sh
ENTRYPOINT ["/opt/bin/munge_entrypoint.sh"]
