FROM alpine:3.22 AS nfs-server

RUN apk add --no-cache --update \
    nfs-utils \
    bash \
    iproute2 && \
    rm -rf /var/cache/apk /tmp && \
    # Create necessary NFS directories
    mkdir -p /var/lib/nfs/rpc_pipefs /var/lib/nfs/v4recovery /var/lib/nfs/sm && \
    # Setup fstab for NFS filesystems
    echo "rpc_pipefs /var/lib/nfs/rpc_pipefs rpc_pipefs defaults 0 0" >> /etc/fstab && \
    echo "nfsd /proc/fs/nfsd nfsd defaults 0 0" >> /etc/fstab && \
    # Explicitly delete the default exports config
    rm -f /etc/exports

COPY images/nfs-server/nfsd.sh /usr/bin/nfsd.sh

RUN chmod +x /usr/bin/nfsd.sh

EXPOSE 2049/tcp 111/tcp 111/udp 20048/tcp

ENTRYPOINT ["/usr/bin/nfsd.sh"]
