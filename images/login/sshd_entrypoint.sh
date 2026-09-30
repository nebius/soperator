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
        if [ "${SOPERATOR_DOCKER_ENABLED:-false}" = "true" ]; then
            echo "Login Docker: ${conf} is required" >&2
            return 1
        fi
        echo "User isolation: ${conf} not found, skipping"
        return 0
    fi
    # shellcheck disable=SC1090
    . "${conf}" || return 1
    if [ "${SOPERATOR_USER_ISOLATION_ENABLED:-false}" != "true" ]; then
        if [ "${SOPERATOR_DOCKER_ENABLED:-false}" = "true" ]; then
            echo "Login Docker: user isolation must be enabled" >&2
            return 1
        fi
        echo "User isolation: disabled"
        return 0
    fi

    # The feature requires a writable cgroup v2 mount (cgroup v1 hosts are not supported).
    if [ "$(stat -f -c %T "${cgroup_mount}" 2>/dev/null)" != "cgroup2fs" ]; then
        echo "User isolation: no cgroup v2 mount at ${cgroup_mount}"
        return 1
    fi

    # Resolve this container's own cgroup: with a host cgroup namespace,
    # /sys/fs/cgroup is the node's root tree and must not be touched directly.
    local cgroup_base cgroup_relative
    cgroup_relative="$(sed -n 's/^0:://p' /proc/self/cgroup)"
    if [ -z "${cgroup_relative}" ]; then
        echo "User isolation: cannot resolve the container cgroup"
        return 1
    fi
    cgroup_base="${cgroup_mount}${cgroup_relative}"
    cgroup_base="${cgroup_base%/}"
    if [ ! -d "${cgroup_base}" ] || [ ! -w "${cgroup_base}/cgroup.procs" ]; then
        echo "User isolation: container cgroup ${cgroup_base} is not writable"
        return 1
    fi

    # cgroup v2 "no internal processes" rule: move all processes into init/
    # before enabling controllers. Retry until empty — a leftover PID makes
    # the subtree_control writes fail with EBUSY.
    mkdir -p "${cgroup_base}/init" "${cgroup_base}/users"
    if [ "${SOPERATOR_DOCKER_ENABLED:-false}" = "true" ]; then
        mkdir -p "${cgroup_base}/docker-unattributed"
    fi
    local pid
    for _ in 1 2 3 4 5; do
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
        echo "User isolation: processes remain in container cgroup after retries"
        return 1
    fi

    # Enable controllers separately: a combined write is atomic and one
    # unavailable controller would fail the other too.
    local controller
    for controller in memory cpu; do
        if ! echo "+${controller}" > "${cgroup_base}/cgroup.subtree_control" 2>/dev/null; then
            echo "User isolation: cannot enable ${controller} controller at ${cgroup_base}"
            return 1
        fi
        if ! echo "+${controller}" > "${cgroup_base}/users/cgroup.subtree_control" 2>/dev/null; then
            echo "User isolation: cannot enable ${controller} controller for users/"
            return 1
        fi
        if ! grep -qw "${controller}" "${cgroup_base}/users/cgroup.subtree_control"; then
            echo "User isolation: ${controller} controller not enabled for users/"
            return 1
        fi
        if [ "${SOPERATOR_DOCKER_ENABLED:-false}" = "true" ]; then
            if ! echo "+${controller}" > "${cgroup_base}/docker-unattributed/cgroup.subtree_control" 2>/dev/null; then
                echo "Login Docker: cannot enable ${controller} controller for unattributed workloads"
                return 1
            fi
        fi
    done

    # The pids controller is useful for Docker, but some Kubernetes runtimes do
    # not delegate it. Keep memory and CPU as the required baseline.
    if [ "${SOPERATOR_DOCKER_ENABLED:-false}" = "true" ] && \
        grep -qw pids "${cgroup_base}/cgroup.controllers"; then
        echo "+pids" > "${cgroup_base}/cgroup.subtree_control" 2>/dev/null || true
        echo "+pids" > "${cgroup_base}/users/cgroup.subtree_control" 2>/dev/null || true
        echo "+pids" > "${cgroup_base}/docker-unattributed/cgroup.subtree_control" 2>/dev/null || true
    fi

    # The sentinel activates the PAM hook; its content is the cgroup base path.
    echo "${cgroup_base}" > /run/soperator-user-isolation.ready
    echo "User isolation: per-user cgroup delegation is ready at ${cgroup_base}"
    if [ "${SOPERATOR_DOCKER_ENABLED:-false}" = "true" ]; then
        echo "${cgroup_relative%/}" > /run/soperator-docker-cgroup-base
        echo "Login Docker: cgroup routing is ready below ${cgroup_base}"
    fi
}
setup_user_isolation

prepare_login_docker() {
    if [ "${SOPERATOR_DOCKER_ENABLED:-false}" != "true" ]; then
        return 0
    fi
    if ! mountpoint -q /mnt/image-storage; then
        echo "Login Docker: /mnt/image-storage is not a mount point" >&2
        return 1
    fi
    if ! install -d -m 0711 /mnt/image-storage/docker; then
        echo "Login Docker: cannot prepare /mnt/image-storage/docker" >&2
        return 1
    fi
    echo "Login Docker: data root is ready at /mnt/image-storage/docker"
}
prepare_login_docker

# TODO: Since 1.29 kubernetes supports native sidecar containers. We can remove it in feature releases
echo "Waiting until munge started"
while [ ! -S "/run/munge/munge.socket.2" ]; do sleep 2; done

source_sshd_config_dir="/mnt/ssh-configs"
effective_sshd_config_dir=$(mktemp -d /run/soperator-ssh-configs.XXXXXX)
/opt/bin/slurm/prepare_sshd_pam_jail_config.sh \
    "${source_sshd_config_dir}" \
    "${effective_sshd_config_dir}"
mount --bind "${effective_sshd_config_dir}" "${source_sshd_config_dir}"
/usr/sbin/sshd -t -f "${source_sshd_config_dir}/sshd_config"

if [ "${SOPERATOR_DOCKER_ENABLED:-false}" = "true" ]; then
    echo "Start sshd, dockerd, and Docker proxy under supervisord"
    exec /usr/bin/supervisord -c /etc/supervisor/soperator-login.conf
fi
echo "Start sshd daemon"
exec /usr/sbin/sshd -D -e -f "${source_sshd_config_dir}/sshd_config"
