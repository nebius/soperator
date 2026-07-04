#!/bin/sh

set -eox

SENTINEL="/mnt/jail/etc/soperator-jail-populated"

while ! mountpoint -q /mnt/jail; do
    echo "Waiting until /mnt/jail is mounted"
    sleep 2
done

populate_jail_rootfs() {
    echo "Populate jail rootfs from a restic backup"
    restic --repo /jail_restic --insecure-no-password restore latest --target /mnt/jail \
      --overwrite always --delete \
      --no-cache --no-extra-verify --option local.connections=64 \
      --json \
      --exclude-xattr system.nfs4_acl

    echo "Set permissions for jail directory"
    chmod 755 /mnt/jail # Permissions 755 are only allowed permissions for OpenSSH ChrootDirectory feature

    # TODO: Move this to an active check/action when it's implemented
    echo "Generate an internal SSH keypair for user root"
    mkdir -p /mnt/jail/root/.ssh
    ssh-keygen -t ecdsa -f /mnt/jail/root/.ssh/id_ecdsa -N "" && cat /mnt/jail/root/.ssh/id_ecdsa.pub >> /mnt/jail/root/.ssh/authorized_keys

    echo "Writing sentinel file"
    date -Iseconds > "$SENTINEL"
}

remove_empty_lib_mount_targets() {
    echo "Removing the flag file that shows that GPU library bind-mount targets exist"
    rm -f "/mnt/jail/etc/gpu_libs_installed.flag"

    echo "Removing empty library files that were used as bind-mount targets on the previous cluster"
    ARCH_LIST="x86_64 aarch64"
    for arch in $ARCH_LIST; do
        dir="/mnt/jail/lib/${arch}-linux-gnu"
        [ -d "$dir" ] || continue
        find "$dir" \
            -maxdepth 1 -type f -empty -name '*.so*' -print |
        while IFS= read -r file; do
            echo "Removing $file"
            rm -- "$file" || true
        done
    done
}

if [ "${OVERWRITE:-}" = "1" ]; then
    echo "Content overwriting is turned on, repopulating jail directory"
    populate_jail_rootfs
elif [ -f "$SENTINEL" ]; then
    echo "Jail directory is already populated (sentinel exists), removing empty libs and exiting"
    remove_empty_lib_mount_targets
    exit 0
elif [ -f "/mnt/jail/.populated" ]; then
    # Migration: jail was populated by an older version that created sentinel file in another location.
    # Write it now so sconfigcontroller can proceed, and avoid unnecessary re-population.
    # Also, delete the old sentinel file.
    echo "Jail looks already populated (legacy, old sentinel), removing empty libs, migrating sentinel and exiting"
    remove_empty_lib_mount_targets
    date -Iseconds > "$SENTINEL"
    rm -f "/mnt/jail/.populated"
    exit 0
elif [ -d /mnt/jail/dev ] && [ -d /mnt/jail/usr ]; then
    # Migration: jail was populated by an older version that didn't write the sentinel.
    # Write it now so sconfigcontroller can proceed, and avoid unnecessary re-population.
    echo "Jail looks already populated (legacy, no sentinel), removing empty libs, writing sentinel and exiting"
    remove_empty_lib_mount_targets
    date -Iseconds > "$SENTINEL"
    exit 0
else
    echo "Jail directory is not populated (no sentinel), populating"
    populate_jail_rootfs
fi
