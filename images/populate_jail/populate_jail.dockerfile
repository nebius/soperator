ARG BASE_IMAGE=restic/restic:0.18.0
ARG TARGETARCH

# First stage: untap jail_rootfs.tar
FROM $BASE_IMAGE AS untaped

ARG TARGETARCH

COPY images/jail_rootfs_${TARGETARCH}.tar /jail_rootfs.tar
RUN mkdir /jail && tar -xvf /jail_rootfs.tar -C /jail && \
    restic init --insecure-no-password --repo /jail_restic && \
    cd /jail && \
    restic --insecure-no-password --repo /jail_restic backup ./ \
        --no-scan --no-cache --read-concurrency 16 \
        --compression max --pack-size 8 \
        --host soperator && \
    cd / && \
    rm -rf -- /jail/..?* /jail/.[!.]* /jail/* && rm /jail_rootfs.tar

# Second stage: copy untaped jail environment to the target
FROM $BASE_IMAGE AS populate_jail

COPY --from=untaped /jail_restic /jail_restic

COPY images/populate_jail/populate_jail_entrypoint.sh .
RUN chmod +x ./populate_jail_entrypoint.sh
ENTRYPOINT ["./populate_jail_entrypoint.sh"]
