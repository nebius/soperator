ARG BASE_IMAGE=ubuntu:jammy

FROM $BASE_IMAGE AS populate_jail

ARG DEBIAN_FRONTEND=noninteractive

RUN apt update && apt install -y rclone rsync

COPY jail_rootfs.tar /jail_rootfs.tar

RUN mkdir /jail && tar -xvf /jail_rootfs.tar -C /jail && rm /jail_rootfs.tar

COPY populate_jail/populate_jail_entrypoint.sh .
RUN chmod +x ./populate_jail_entrypoint.sh
ENTRYPOINT ["./populate_jail_entrypoint.sh"]
