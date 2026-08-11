#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code

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

echo "Creating symlink to the slurm configs"
rm -rf /etc/slurm && ln -s /mnt/jail/etc/slurm /etc/slurm

echo "Link soperator home directories from jail to use SSH keys from there"
mkdir -p /mnt/jail/opt/soperator-home
ln -s /mnt/jail/opt/soperator-home /opt/soperator-home

echo "Create privilege separation directory /var/run/sshd"
mkdir -p /var/run/sshd

echo "Complement jail rootfs"
/opt/bin/slurm/complement_jail.sh -j /mnt/jail -u /mnt/jail.upper

echo "Set up per-user cgroup isolation for SSH sessions (if enabled)"
setup_user_isolation() {
    local conf="/etc/soperator/user-isolation.conf"
    local cgroup_mount="/sys/fs/cgroup"

    if [ ! -f "${conf}" ]; then
        echo "User isolation: ${conf} not found, skipping"
        return 0
    fi
    # shellcheck disable=SC1090
    . "${conf}" || return 1
    if [ "${SOPERATOR_USER_ISOLATION_ENABLED:-false}" != "true" ]; then
        echo "User isolation: disabled"
        return 0
    fi

    # The feature requires a writable cgroup v2 mount (cgroup v1 hosts are not supported).
    if [ "$(stat -f -c %T "${cgroup_mount}" 2>/dev/null)" != "cgroup2fs" ]; then
        echo "User isolation: no cgroup v2 mount at ${cgroup_mount}, feature disabled"
        return 0
    fi

    # Resolve this container's own cgroup: with a host cgroup namespace,
    # /sys/fs/cgroup is the node's root tree and must not be touched directly.
    local cgroup_base
    cgroup_base="${cgroup_mount}$(sed -n 's/^0:://p' /proc/self/cgroup)"
    cgroup_base="${cgroup_base%/}"
    if [ ! -d "${cgroup_base}" ] || [ ! -w "${cgroup_base}/cgroup.procs" ]; then
        echo "User isolation: container cgroup ${cgroup_base} is not writable, feature disabled"
        return 0
    fi

    # cgroup v2 "no internal processes" rule: move all processes into init/
    # before enabling controllers. Retry until empty — a leftover PID makes
    # the subtree_control writes fail with EBUSY.
    mkdir -p "${cgroup_base}/init" "${cgroup_base}/users"
    local attempt pid
    for attempt in 1 2 3 4 5; do
        while IFS= read -r pid; do
            echo "${pid}" > "${cgroup_base}/init/cgroup.procs" 2>/dev/null || true
        done < "${cgroup_base}/cgroup.procs"

        # cgroup control files do not expose a reliable file size, so check
        # emptiness by reading the file.
        if ! IFS= read -r pid < "${cgroup_base}/cgroup.procs"; then
            break
        fi
    done

    # Do not attempt to enable domain controllers while processes remain in
    # the parent cgroup.
    if IFS= read -r pid < "${cgroup_base}/cgroup.procs"; then
        echo "User isolation: processes remain in container cgroup after retries, feature disabled"
        return 0
    fi

    # Enable controllers separately: a combined write is atomic and one
    # unavailable controller would fail the other too.
    local controller
    for controller in memory cpu; do
        echo "+${controller}" > "${cgroup_base}/cgroup.subtree_control" 2>/dev/null \
            || echo "User isolation: cannot enable ${controller} controller at ${cgroup_base}"
        echo "+${controller}" > "${cgroup_base}/users/cgroup.subtree_control" 2>/dev/null \
            || echo "User isolation: cannot enable ${controller} controller for users/"
    done

    # Without the memory controller, limits would be unenforced, so do not activate isolation.
    if ! grep -qw memory "${cgroup_base}/users/cgroup.subtree_control"; then
        echo "User isolation: memory controller not enabled, feature disabled"
        return 0
    fi

    # The sentinel activates the PAM hook; its content is the cgroup base path.
    echo "${cgroup_base}" > /run/soperator-user-isolation.ready
    echo "User isolation: per-user cgroup delegation is ready at ${cgroup_base}"
}
setup_user_isolation || echo "User isolation: setup failed, continuing without isolation"

# TODO: Since 1.29 kubernetes supports native sidecar containers. We can remove it in feature releases
echo "Waiting until munge started"
while [ ! -S "/run/munge/munge.socket.2" ]; do sleep 2; done

echo "Start sshd daemon"
exec /usr/sbin/sshd -D -e -f /mnt/ssh-configs/sshd_config
