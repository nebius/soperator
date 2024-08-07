FROM ubuntu:focal AS munge

ARG DEBIAN_FRONTEND=noninteractive

# Install munge
COPY docker/common/scripts/install_munge.sh /opt/bin/
RUN chmod +x /opt/bin/install_munge.sh && \
    /opt/bin/install_munge.sh && \
    rm /opt/bin/install_munge.sh

# Update linker cache
RUN ldconfig

# Delete users & home because they will be linked from jail
RUN rm /etc/passwd* /etc/group* /etc/shadow* /etc/gshadow*
RUN rm -rf /home

ENV MUNGE_NUM_THREADS=10
ENV MUNGE_KEY_FILE=/etc/munge/munge.key
ENV MUNGE_PID_FILE=/run/munge/munged.pid
ENV MUNGE_SOCKET_FILE=/run/munge/munge.socket.2

# Copy & run the entrypoint script
COPY docker/munge/munge_entrypoint.sh /opt/bin/
RUN chmod +x /opt/bin/munge_entrypoint.sh
ENTRYPOINT /opt/bin/munge_entrypoint.sh
