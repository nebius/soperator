#!/bin/bash

set -eox

while ! mountpoint -q /mnt/jail; do
    echo "Waiting until /mnt/jail is mounted"
    sleep 10
done

populate_jail_rootfs() {
    rclone_transfers=32
    rclone_checkers=24
    rsync_procs=16

    echo "Delete everything from jail directory"
    rclone purge /mnt/jail/ --checkers="$rclone_checkers" || true

    echo "Set permissions for jail directory"
    chown 0:0 /mnt/jail
    chmod 755 /mnt/jail # Permissions 755 are only allowed permissions for OpenSSH ChrootDirectory feature

    echo "Rclone jail rootfs into jail directory"
    rclone copy /jail /mnt/jail --progress --links \
        --transfers="$rclone_transfers" --buffer-size=128Mi \
        --checkers="$rclone_checkers"

    echo "Fix permissions and symlinks using rsync"
    rsync_opts=(
      --verbose
      -aHAX # archive + hard-links + acls + xattrs
      --no-sparse
      --one-file-system
      --numeric-ids # keep UIDs/GIDs
    )
    rsync "${rsync_opts[@]}" \
      --include='*/' \
      --include='*/*/' \
      --include='*/*' \
      --exclude='*/*/*' \
      /jail /mnt/jail/
    cd /jail
    find . -mindepth 2 -maxdepth 2 -type d -print0 \
      | xargs -0 -n1 -P"$rsync_procs" -I{} bash -c '
          # {} is like "./foo/bar"
          dst_dir="/mnt/jail/${1#./}"
          mkdir -p "$dst_dir"
          rsync "${rsync_opts[@]}" "/${1#./}/" "$dst_dir/"
        ' _ {}
    #rsync --verbose -aHAX --no-sparse --one-file-system /jail/ /mnt/jail/

    # TODO: Move this to an active check/action when it's implemented
    echo "Generate an internal SSH keypair for user root"
    apt update -y
    apt install -y openssh-client
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
        echo "Jail directory is filled with something that doesn't resemble a rootfs, failing"
        exit 1
    fi
fi
