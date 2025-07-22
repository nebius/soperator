ARG BASE_IMAGE=cr.eu-north1.nebius.cloud/soperator/ubuntu:noble

# First stage: untap jail_rootfs.tar
FROM $BASE_IMAGE AS untaped
COPY images/jail_rootfs.tar /jail_rootfs.tar
RUN mkdir /jail && \
    tar -xvf /jail_rootfs.tar -C /jail && \
    rm /jail_rootfs.tar

# Second stage: copy untaped jail environment to the target
FROM $BASE_IMAGE AS populate_jail

ARG DEBIAN_FRONTEND=noninteractive

RUN apt update && \
    apt install -y rsync && \
    apt clean

# Install Rclone
COPY images/common/scripts/install_rclone.sh /opt/bin/
RUN chmod +x /opt/bin/install_rclone.sh && \
    /opt/bin/install_rclone.sh && \
    rm /opt/bin/install_rclone.sh

COPY --from=untaped /jail /jail

COPY images/populate_jail/populate_jail_entrypoint.sh .
RUN chmod +x ./populate_jail_entrypoint.sh
ENTRYPOINT ["./populate_jail_entrypoint.sh"]
