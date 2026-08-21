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

# Keep ChrootDirectory in the operator-rendered config for compatibility with
# older login images. This image uses the PAM mount-namespace jail instead.
echo "Prepare sshd config for PAM jail"
effective_sshd_config="/run/soperator-sshd_config"
awk '
    {
        keyword = $1
        sub(/=.*/, "", keyword)
        if (tolower(keyword) != "chrootdirectory") {
            print
        }
    }
' /mnt/ssh-configs/sshd_config > "${effective_sshd_config}"
chmod 0600 "${effective_sshd_config}"

echo "Set up the cgroup hierarchy for login workloads (if required)"
setup_cgroup_hierarchy() {
    local conf="/etc/soperator/user-isolation.conf"
    local cgroup_mount="/sys/fs/cgroup"
    local docker_enabled="${SOPERATOR_DOCKER_ENABLED:-false}"
    local user_isolation_enabled=false
    local users_cgroup_enabled=false

    if [ -f "${conf}" ]; then
        # shellcheck disable=SC1090
        . "${conf}" || return 1
        user_isolation_enabled="${SOPERATOR_USER_ISOLATION_ENABLED:-false}"
    elif [ "${docker_enabled}" != "true" ]; then
        echo "User isolation: ${conf} not found, skipping"
    fi

    if [ "${user_isolation_enabled}" != "true" ] && [ "${docker_enabled}" != "true" ]; then
        [ -f "${conf}" ] && echo "User isolation: disabled"
        return 0
    fi
    if [ "${user_isolation_enabled}" = "true" ] || [ "${docker_enabled}" = "true" ]; then
        users_cgroup_enabled=true
    fi

    # The feature requires a writable cgroup v2 mount (cgroup v1 hosts are not supported).
    if [ "$(stat -f -c %T "${cgroup_mount}" 2>/dev/null)" != "cgroup2fs" ]; then
        echo "Login cgroups: no cgroup v2 mount at ${cgroup_mount}"
        return 1
    fi

    # Resolve this container's own cgroup: with a host cgroup namespace,
    # /sys/fs/cgroup is the node's root tree and must not be touched directly.
    local cgroup_base cgroup_relative
    cgroup_relative="$(sed -n 's/^0:://p' /proc/self/cgroup)"
    if [ -z "${cgroup_relative}" ]; then
        echo "Login cgroups: cannot resolve the container cgroup"
        return 1
    fi
    cgroup_base="${cgroup_mount}${cgroup_relative}"
    cgroup_base="${cgroup_base%/}"
    if [ ! -d "${cgroup_base}" ] || [ ! -w "${cgroup_base}/cgroup.procs" ]; then
        echo "Login cgroups: container cgroup ${cgroup_base} is not writable"
        return 1
    fi

    # cgroup v2 "no internal processes" rule: move all processes into init/
    # before enabling controllers. Retry until empty — a leftover PID makes
    # the subtree_control writes fail with EBUSY.
    mkdir -p "${cgroup_base}/init"
    if [ "${users_cgroup_enabled}" = "true" ]; then
        mkdir -p "${cgroup_base}/users"
    fi
    if [ "${docker_enabled}" = "true" ]; then
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
        echo "Login cgroups: processes remain in container cgroup after retries"
        return 1
    fi

    # Enable controllers separately: a combined write is atomic and one
    # unavailable controller would fail the other too.
    local controller
    for controller in memory cpu; do
        if ! grep -qw "${controller}" "${cgroup_base}/cgroup.controllers"; then
            echo "Login cgroups: ${controller} controller is not delegated to ${cgroup_base}"
            return 1
        fi
        if ! echo "+${controller}" > "${cgroup_base}/cgroup.subtree_control" 2>/dev/null; then
            echo "Login cgroups: cannot enable ${controller} controller at ${cgroup_base}"
            return 1
        fi
        if [ "${users_cgroup_enabled}" = "true" ]; then
            if ! echo "+${controller}" > "${cgroup_base}/users/cgroup.subtree_control" 2>/dev/null; then
                echo "User isolation: cannot enable ${controller} controller for users/"
                return 1
            fi
            if ! grep -qw "${controller}" "${cgroup_base}/users/cgroup.subtree_control"; then
                echo "User isolation: ${controller} controller not enabled for users/"
                return 1
            fi
        fi
        if [ "${docker_enabled}" = "true" ]; then
            if ! echo "+${controller}" > "${cgroup_base}/docker-unattributed/cgroup.subtree_control" 2>/dev/null; then
                echo "Login Docker: cannot enable ${controller} controller for unattributed workloads"
                return 1
            fi
            if ! grep -qw "${controller}" "${cgroup_base}/docker-unattributed/cgroup.subtree_control"; then
                echo "Login Docker: ${controller} controller not enabled for unattributed workloads"
                return 1
            fi
        fi
    done

    # Delegate the PID controller when Kubernetes makes it available. CPU and
    # memory remain the required baseline for both isolation and Docker.
    if grep -qw pids "${cgroup_base}/cgroup.controllers"; then
        echo "+pids" > "${cgroup_base}/cgroup.subtree_control"
        if [ "${users_cgroup_enabled}" = "true" ]; then
            echo "+pids" > "${cgroup_base}/users/cgroup.subtree_control"
        fi
        if [ "${docker_enabled}" = "true" ]; then
            echo "+pids" > "${cgroup_base}/docker-unattributed/cgroup.subtree_control"
        fi
    fi

    if [ "${user_isolation_enabled}" = "true" ]; then
        # The sentinel activates the PAM hook; its content is the resolved base.
        echo "${cgroup_base}" > /run/soperator-user-isolation.ready
        echo "User isolation: per-user cgroup delegation is ready at ${cgroup_base}"
    fi
    if [ "${docker_enabled}" = "true" ]; then
        echo "${cgroup_relative%/}" > /run/soperator-docker-cgroup-base
        echo "${cgroup_relative%/}/docker-unattributed" > /run/soperator-docker-cgroup-parent
        echo "Login Docker: per-UID and fallback cgroups are ready below ${cgroup_base}"
    fi
}
setup_cgroup_hierarchy

prepare_login_docker() {
    if [ "${SOPERATOR_DOCKER_ENABLED:-false}" != "true" ]; then
        return 0
    fi
    if ! mountpoint -q /mnt/image-storage; then
        echo "Login Docker: /mnt/image-storage is not a dedicated mount" >&2
        return 1
    fi

    install -d -m 0711 /mnt/image-storage/docker
    touch /run/soperator-docker.enabled
    echo "Login Docker: data root is ready at /mnt/image-storage/docker"
}
prepare_login_docker

# TODO: Since 1.29 kubernetes supports native sidecar containers. We can remove it in feature releases
echo "Waiting until munge started"
while [ ! -S "/run/munge/munge.socket.2" ]; do sleep 2; done

if [ "${SOPERATOR_DOCKER_ENABLED:-false}" = "true" ]; then
    # Preserve Kubernetes' initial OOM adjustment for child services, then
    # protect supervisord itself. The service wrappers restore or replace the
    # inherited value before execing sshd and dockerd respectively.
    cat /proc/self/oom_score_adj > /run/soperator-default-oom-score-adj
    if ! echo -1000 > /proc/self/oom_score_adj 2>/dev/null; then
        echo "Login Docker: cannot protect supervisord from container-level OOM" >&2
        exit 1
    fi
    echo "Start sshd and dockerd under supervisord"
    exec /usr/bin/supervisord
fi

echo "Start sshd daemon"
exec /usr/sbin/sshd -D -e -f "${effective_sshd_config}"
