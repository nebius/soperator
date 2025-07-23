ARG BASE_IMAGE=cr.eu-north1.nebius.cloud/soperator/ubuntu:noble
ARG TARGETARCH

FROM $BASE_IMAGE AS populate_jail

ARG DEBIAN_FRONTEND=noninteractive

RUN apt update && \
    apt install -y rclone rsync && \
    apt clean

ADD --link images/jail_rootfs_${TARGETARCH}.tar /jail/

COPY images/populate_jail/populate_jail_entrypoint.sh .
RUN chmod +x ./populate_jail_entrypoint.sh
ENTRYPOINT ["./populate_jail_entrypoint.sh"]
