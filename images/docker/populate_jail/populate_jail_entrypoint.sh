#!/bin/bash

echo "Delete everything from jail directory"
rm -rf /mnt/jail/*

echo "Extract jail rootfs tarball into jail directory"
pigz -d -p $(nproc) -c jail_rootfs.tar.gz | tar -xvf - -C /mnt/jail

echo "Set permissions for jail directory"
chmod 755 /mnt/jail # Permissions 755 are only allowed permissions for OpenSSH ChrootDirectory feature
