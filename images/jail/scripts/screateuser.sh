#!/bin/bash

set -e

if [[ $# -eq 0 ]] || [[ "$*" == *"-h "* ]] || [[ "$*" == *"--help"* ]]; then
    echo "Usage: screateuser <username> [--with-password] [--without-sudo] [--without-docker] [<args for adduser...>]"
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

expect_home_value=0
home_dir=$(eval echo "~$username")
for arg in "$@"; do
    if [[ "$expect_home_value" -eq 1 ]]; then
        home_dir="$arg"
        expect_home_value=0
        continue
    fi

    if [[ "$arg" == "--home" ]]; then
        expect_home_value=1
    fi
done

ssh_dir="$home_dir/.ssh"
authorized_keys="$ssh_dir/authorized_keys"
internal_key="$ssh_dir/id_ecdsa"

if [ ! -d "$ssh_dir" ]; then
    mkdir -p "$ssh_dir"
    chown "$username:$username" "$ssh_dir"
    chmod 700 "$ssh_dir"
fi

if [ ! -f "$authorized_keys" ]; then
    touch "$authorized_keys"
    chown "$username:$username" "$authorized_keys"
    chmod 600 "$authorized_keys"
fi

if [ -n "$ssh_public_key" ]; then
    echo "Saving SSH key to '${authorized_keys}' ..."
    echo "$ssh_public_key" >> "$authorized_keys"
fi

echo "Generating an internal SSH key pair ..."
ssh-keygen -t ecdsa -f "$internal_key" -N ''
chown "$username:$username" "$internal_key" "$internal_key.pub"
cat "$internal_key.pub" >> "$authorized_keys"
