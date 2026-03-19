#!/bin/sh

set -eox

SENTINEL="/mnt/jail/.populated"

while ! mountpoint -q /mnt/jail; do
    echo "Waiting until /mnt/jail is mounted"
    sleep 2
done

populate_jail_rootfs() {
    echo "Populate jail rootfs from a restic backup"
    restic --repo /jail_restic --insecure-no-password restore latest --target /mnt/jail \
      --overwrite always --delete \
      --no-cache --no-extra-verify --option local.connections=64 \
      --json

    echo "Set permissions for jail directory"
    chmod 755 /mnt/jail # Permissions 755 are only allowed permissions for OpenSSH ChrootDirectory feature

    # TODO: Move this to an active check/action when it's implemented
    echo "Generate an internal SSH keypair for user root"
    mkdir -p /mnt/jail/root/.ssh
    ssh-keygen -t ecdsa -f /mnt/jail/root/.ssh/id_ecdsa -N "" && cat /mnt/jail/root/.ssh/id_ecdsa.pub >> /mnt/jail/root/.ssh/authorized_keys

    echo "Writing sentinel file"
    date -Iseconds > "$SENTINEL"
}

if [ "${OVERWRITE:-}" = "1" ]; then
    echo "Content overwriting is turned on, repopulating jail directory"
    populate_jail_rootfs
elif [ -f "$SENTINEL" ]; then
    echo "Jail directory is already populated (sentinel exists), exiting"
    exit 0
else
    echo "Jail directory is not populated (no sentinel), populating"
    populate_jail_rootfs
fi
