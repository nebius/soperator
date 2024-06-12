#!/bin/bash

set -e

adduser "$@"

username="$1"

echo "Enter the SSH public key, or press ENTER to avoid creating a key:"
read -r ssh_public_key

usermod -aG sudo "$username"

if [ -n "$ssh_public_key" ]; then
    home_dir=$(eval echo "~$username")
    ssh_dir="$home_dir/.ssh"
    if [ ! -d "$ssh_dir" ]; then
        mkdir -p "$ssh_dir"
        chown "$username:$username" "$ssh_dir"
        chmod 700 "$ssh_dir"
    fi

    authorized_keys="$ssh_dir/authorized_keys"
    echo "$ssh_public_key" >> "$authorized_keys"
    chown "$username:$username" "$authorized_keys"
    chmod 600 "$authorized_keys"
fi
