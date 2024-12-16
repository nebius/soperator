#!/bin/bash

while ! mountpoint -q /mnt/jail; do
    echo "Waiting until /mnt/jail is mounted"
    sleep 10
done

if [ "$OVERWRITE" != "1" ] && [ -d /mnt/jail/dev ]; then
    echo "Jail is already populated and content overwriting is turned off, exiting"
    exit 0
fi

echo "Delete everything from jail directory"
rm -rf /mnt/jail/*

echo "Rclone and rsync jail rootfs into jail directory"
rclone copy /jail /mnt/jail --progress --transfers="$(nproc)" --links
rsync --verbose --archive --one-file-system --xattrs --numeric-ids --sparse --acls --hard-links /jail/ /mnt/jail/

echo "Set permissions for jail directory"
chmod 755 /mnt/jail # Permissions 755 are only allowed permissions for OpenSSH ChrootDirectory feature
