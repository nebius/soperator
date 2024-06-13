FROM ubuntu:focal as populate_jail

ARG DEBIAN_FRONTEND=noninteractive

RUN apt update && apt install -y pigz

COPY docker/jail/jail_rootfs.tar.gz .

COPY docker/populate_jail/populate_jail_entrypoint.sh .
RUN chmod +x ./populate_jail_entrypoint.sh
ENTRYPOINT ./populate_jail_entrypoint.sh
