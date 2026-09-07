#!/bin/bash
# PAM session hook (pam_exec): places each SSH session into a per-user cgroup v2.
# Configured via the SlurmCluster `login.userIsolation` field.
# Must never block a login: every failure path exits 0.

set -u

CONF="/etc/soperator/user-isolation.conf"
READY_SENTINEL="/run/soperator-user-isolation.ready"

# pam_exec is registered with log=/proc/1/fd/1, which routes this hook's output
# to the container log. pam_exec discards child output without log=, and there
# is no syslog daemon in this container for logger(1).
log() {
    echo "soperator-user-isolation: $1" >&2
}

[ "${PAM_TYPE:-}" = "open_session" ] || exit 0

# The sentinel is written by the entrypoint after successful cgroup setup;
# its content is the container's resolved cgroup base path.
[ -f "${READY_SENTINEL}" ] || exit 0
cgroup_base="$(cat "${READY_SENTINEL}" 2>/dev/null)"
[ -n "${cgroup_base}" ] && [ -d "${cgroup_base}/users" ] || exit 0

[ -f "${CONF}" ] || exit 0
# shellcheck disable=SC1090
. "${CONF}" 2>/dev/null || exit 0
[ "${SOPERATOR_USER_ISOLATION_ENABLED:-false}" = "true" ] || exit 0

user="${PAM_USER:-}"
[ -n "${user}" ] || exit 0

uid="$(id -u "${user}" 2>/dev/null)" || exit 0

# Never limit root or system users.
[ "${uid}" -ge 1000 ] || exit 0

user_cg="${cgroup_base}/users/user-${uid}"
session_cg="${user_cg}"
docker_enabled="${SOPERATOR_DOCKER_ENABLED:-false}"

if [ "${docker_enabled}" = "true" ]; then
    session_cg="${user_cg}/sessions"
    if ! mkdir -p "${session_cg}" "${user_cg}/docker" 2>/dev/null; then
        log "cannot create Docker cgroup hierarchy for uid ${uid}; session left unrestricted"
        exit 0
    fi
elif ! mkdir -p "${user_cg}" 2>/dev/null; then
    log "cannot create cgroup for uid ${uid}; session left unrestricted"
    exit 0
fi

# Keep cgroup files root-owned: the tree is visible inside the jail and
# delegation would let users lift their own limits.
if [ -n "${SOPERATOR_USER_ISOLATION_MEMORY_HIGH:-}" ]; then
    echo "${SOPERATOR_USER_ISOLATION_MEMORY_HIGH}" > "${user_cg}/memory.high" 2>/dev/null \
        || log "failed to set memory.high for uid ${uid}"
fi
if [ -n "${SOPERATOR_USER_ISOLATION_MEMORY_MAX:-}" ]; then
    echo "${SOPERATOR_USER_ISOLATION_MEMORY_MAX}" > "${user_cg}/memory.max" 2>/dev/null \
        || log "failed to set memory.max for uid ${uid}"
fi
if [ -n "${SOPERATOR_USER_ISOLATION_CPU_WEIGHT:-}" ]; then
    echo "${SOPERATOR_USER_ISOLATION_CPU_WEIGHT}" > "${user_cg}/cpu.weight" 2>/dev/null \
        || log "failed to set cpu.weight for uid ${uid}"
fi

if [ "${docker_enabled}" = "true" ]; then
    # Keep user-<uid> free of processes so resource controllers can be
    # delegated to both the SSH-session leaf and Docker's child cgroups.
    for controller in memory cpu pids; do
        if ! grep -qw "${controller}" "${user_cg}/cgroup.controllers"; then
            continue
        fi
        if ! echo "+${controller}" > "${user_cg}/cgroup.subtree_control" 2>/dev/null; then
            log "failed to enable ${controller} for nested workloads of uid ${uid}"
            continue
        fi
        if ! echo "+${controller}" > "${user_cg}/docker/cgroup.subtree_control" 2>/dev/null; then
            log "failed to enable ${controller} for Docker workloads of uid ${uid}"
        fi
    done
fi

# Move the parent sshd session process; everything it spawns inherits membership.
if ! echo "${PPID}" > "${session_cg}/cgroup.procs" 2>/dev/null; then
    log "failed to move session of uid ${uid} into ${session_cg}"
fi

exit 0
