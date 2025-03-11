#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code

# Parse command line arguments
HOOK_SCRIPT=""
while [[ $# -gt 0 ]]; do
    case $1 in
        --hook-script)
            HOOK_SCRIPT="$2"
            shift 2
            ;;
        *)
            shift
            ;;
    esac
done

echo "Link users from jail"
ln -s /mnt/jail/etc/passwd /etc/passwd
ln -s /mnt/jail/etc/group /etc/group
ln -s /mnt/jail/etc/shadow /etc/shadow
ln -s /mnt/jail/etc/gshadow /etc/gshadow
chown -h 0:42 /etc/{shadow,gshadow}

echo "Link SSH \"message of the day\" scripts from jail"
ln -s /mnt/jail/etc/update-motd.d /etc/update-motd.d

echo "Link home from jail to use SSH keys from there"
ln -s /mnt/jail/home /home

echo "Create privilege separation directory /var/run/sshd"
mkdir -p /var/run/sshd

echo "Complement jail rootfs"
/opt/bin/slurm/complement_jail.sh -j /mnt/jail -u /mnt/jail.upper

# TODO: Since 1.29 kubernetes supports native sidecar containers. We can remove it in feature releases
echo "Waiting until munge started"
while [ ! -S "/run/munge/munge.socket.2" ]; do sleep 2; done

# Execute hook script if provided
if [ -n "${HOOK_SCRIPT}" ]; then
    # Exit early if the file doesn't exist
    if [ ! -f "${HOOK_SCRIPT}" ]; then
        echo "Hook script file '${HOOK_SCRIPT}' does not exist. Exiting."
        exit 1
    fi

    echo "Executing hook script: ${HOOK_SCRIPT}"
    set +e
    if [ -x "${HOOK_SCRIPT}" ]; then
        "${HOOK_SCRIPT}"
    else
        bash "${HOOK_SCRIPT}"
    fi

    exit_code=$?
    set -e

    if [ $exit_code -ne 0 ]; then
        echo "Hook script failed with exit code $exit_code. Exiting."
        exit 1
    fi
fi

echo "Start sshd daemon"
/usr/sbin/sshd -D -e -f /mnt/ssh-configs/sshd_config
