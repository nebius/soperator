FROM cr.eu-north1.nebius.cloud/soperator/ubuntu:noble AS munge

ARG DEBIAN_FRONTEND=noninteractive

# Install munge
COPY images/common/scripts/install_munge.sh /opt/bin/
RUN chmod +x /opt/bin/install_munge.sh && \
    /opt/bin/install_munge.sh && \
    rm /opt/bin/install_munge.sh

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
