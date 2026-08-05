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

    # Operate on this container's OWN cgroup, never on the mount root: with a host
    # cgroup namespace (used by some runtimes for privileged pods), /sys/fs/cgroup
    # is the node's root tree, and touching it would affect the whole node. In a
    # private cgroup namespace /proc/self/cgroup is "0::/" and the base resolves
    # to the mount root, which is then genuinely ours.
    local cgroup_base
    cgroup_base="${cgroup_mount}$(sed -n 's/^0:://p' /proc/self/cgroup)"
    cgroup_base="${cgroup_base%/}"
    if [ ! -d "${cgroup_base}" ] || [ ! -w "${cgroup_base}/cgroup.procs" ]; then
        echo "User isolation: container cgroup ${cgroup_base} is not writable, feature disabled"
        return 0
    fi

    # cgroup v2 forbids a cgroup from having both processes and controller-enabled
    # children ("no internal processes" rule): park this shell (and thus sshd,
    # exec'ed later) in init/ before enabling controllers for the users/ subtree.
    mkdir -p "${cgroup_base}/init" "${cgroup_base}/users"
    local pid
    while read -r pid; do
        echo "${pid}" > "${cgroup_base}/init/cgroup.procs" 2>/dev/null || true
    done < "${cgroup_base}/cgroup.procs"

    echo "+memory +cpu" > "${cgroup_base}/cgroup.subtree_control"
    echo "+memory +cpu" > "${cgroup_base}/users/cgroup.subtree_control"

    # The PAM session hook only starts placing sessions once this sentinel exists;
    # its content tells the hook which cgroup subtree to place them into.
    echo "${cgroup_base}" > /run/soperator-user-isolation.ready
    echo "User isolation: per-user cgroup delegation is ready at ${cgroup_base}"
}
setup_user_isolation || echo "User isolation: setup failed, continuing without isolation"

# TODO: Since 1.29 kubernetes supports native sidecar containers. We can remove it in feature releases
echo "Waiting until munge started"
while [ ! -S "/run/munge/munge.socket.2" ]; do sleep 2; done

echo "Start sshd daemon"
exec /usr/sbin/sshd -D -e -f /mnt/ssh-configs/sshd_config
