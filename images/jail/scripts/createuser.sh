#!/bin/bash

set -e

if [[ $# -eq 0 ]] || [[ "$*" == *"-h"* ]] || [[ "$*" == *"--help"* ]]; then
    echo "Usage: createuser <username> [--with-password] [--without-sudo] [--without-docker]"
    echo "       [--without-ssh-key] [<args for adduser...>]"
    exit 0
fi

# Set "--disabled-password" by default
if [[ "$*" != *"--disabled-password"* ]] && [[ "$*" != *"--with-password"* ]]; then
    set -- "$@" --disabled-password
fi

ARGS=("$@")
ADDUSER_ARGS=()
for arg in "${ARGS[@]}"; do
    if [[ "$arg" != "--with-password" ]] && \
       [[ "$arg" != "--without-sudo" ]] && \
       [[ "$arg" != "--without-docker" ]]; then
        ADDUSER_ARGS+=("$arg")
    fi
done

adduser "${ADDUSER_ARGS[@]}"

username="$1"

echo "Enter SSH public key, or press ENTER to avoid creating a key:"
read -r ssh_public_key

add_to_group() {
    local grp=$1
    if getent group docker >/dev/null; then
        echo "Adding user '${username}' to group '${grp}' ..."
    fi
    usermod -aG "${grp}" "${username}"
}

if [[ "$*" != *"--without-sudo"* ]]; then
    add_to_group sudo
fi

if [[ "$*" != *"--without-docker"* ]]; then
    add_to_group docker
fi

if [ -n "$ssh_public_key" ]; then
    home_dir=$(eval echo "~$username")
    ssh_dir="$home_dir/.ssh"
    if [ ! -d "$ssh_dir" ]; then
        mkdir -p "$ssh_dir"
        chown "$username:$username" "$ssh_dir"
        chmod 700 "$ssh_dir"
    fi

    authorized_keys="$ssh_dir/authorized_keys"
    echo "Saving SSH key to '${authorized_keys}'"
    echo "$ssh_public_key" >> "$authorized_keys"
    chown "$username:$username" "$authorized_keys"
    chmod 600 "$authorized_keys"
fi
