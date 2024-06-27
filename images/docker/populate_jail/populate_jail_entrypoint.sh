#!/bin/bash

echo "Delete everything from jail directory"
rm -rf /mnt/jail/*

echo "Rclone and rsync jail rootfs into jail directory"
rclone copy /jail /mnt/jail --progress --multi-thread-streams="$(nproc)" --links
rsync --verbose --archive --one-file-system --xattrs --numeric-ids --sparse --acls --hard-links /jail/ /mnt/jail/

echo "Set permissions for jail directory"
chmod 755 /mnt/jail # Permissions 755 are only allowed permissions for OpenSSH ChrootDirectory feature
