#!/bin/sh

set -eox

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
    chown 0:0 /mnt/jail

    # TODO: Move this to an active check/action when it's implemented
    echo "Generate an internal SSH keypair for user root"
    mkdir -p /mnt/jail/root/.ssh
    ssh-keygen -t ecdsa -f /mnt/jail/root/.ssh/id_ecdsa -N "" && cat /mnt/jail/root/.ssh/id_ecdsa.pub >> /mnt/jail/root/.ssh/authorized_keys
}

if [ "${OVERWRITE:-}" = "1" ]; then
    echo "Content overwriting is turned on, repopulating jail directory"
    populate_jail_rootfs
else
    echo "Content overwriting is turned off"
    if [ -z "$(ls -A /mnt/jail)" ]; then
        echo "Jail directory is empty, populating"
        populate_jail_rootfs
    elif [ -d /mnt/jail/dev ] || [ -d /mnt/jail/etc ] || [ -d /mnt/jail/usr ]; then
        echo "Jail directory is already populated with something that resembles a rootfs, exiting"
        exit 0
    else
        echo "Jail directory is filled with something that does not resemble a rootfs, failing"
        exit 1
    fi
fi
