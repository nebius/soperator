FROM ubuntu:focal as populate_jail

ARG DEBIAN_FRONTEND=noninteractive

RUN apt update && apt install -y rclone rsync

ADD jail /jail

COPY docker/populate_jail/populate_jail_entrypoint.sh .
RUN chmod +x ./populate_jail_entrypoint.sh
ENTRYPOINT ./populate_jail_entrypoint.sh
