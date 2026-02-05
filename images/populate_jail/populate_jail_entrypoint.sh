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

    # TODO: Move this to an active check/action when it's implemented
    echo "Generate an internal SSH keypair for user root"
    mkdir -p /mnt/jail/root/.ssh
    ssh-keygen -t ecdsa -f /mnt/jail/root/.ssh/id_ecdsa -N "" && cat /mnt/jail/root/.ssh/id_ecdsa.pub >> /mnt/jail/root/.ssh/authorized_keys
}

remove_empty_lib_mount_targets() {
    echo "Removing the flag file that shows that GPU library bind-mount targets exist"
    rm "/mnt/jail/etc/gpu_libs_installed.flag"

    echo "Removing empty library files that were used as bind-mount targets on the previous cluster"
    ARCH_LIST="x86_64 aarch64"
    for arch in $ARCH_LIST; do
        find "/mnt/jail/lib/${arch}-linux-gnu" \
            -maxdepth 1 -type f ! -type l -empty -print |
        while IFS= read -r file; do
            echo "Removing $file"
            rm -- "$file"
        done
    done
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
        echo "Jail directory is already populated with something that resembles a rootfs, removing empty libs"
        remove_empty_lib_mount_targets
        exit 0
    else
        echo "Jail directory is filled with something that does not resemble a rootfs, failing"
        exit 1
    fi
fi
