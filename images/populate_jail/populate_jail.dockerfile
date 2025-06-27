ARG BASE_IMAGE=cr.eu-north1.nebius.cloud/soperator/ubuntu:jammy

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
    apt install -y rclone rsync && \
    apt clean

COPY --from=untaped /jail /jail

COPY images/populate_jail/populate_jail_entrypoint.sh .
RUN chmod +x ./populate_jail_entrypoint.sh
ENTRYPOINT ["./populate_jail_entrypoint.sh"]
