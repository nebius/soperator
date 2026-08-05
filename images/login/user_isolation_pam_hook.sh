#!/bin/bash
# PAM session hook (pam_exec) that places each SSH session into a per-user
# cgroup v2, so one user cannot exhaust the memory or monopolize the CPU of
# the whole login node. Configured via the SlurmCluster `login.userIsolation`
# field, delivered as /etc/soperator/user-isolation.conf.
#
# This hook must never block a login: every failure path exits 0 and leaves
# the session unrestricted.

set -u

CONF="/etc/soperator/user-isolation.conf"
READY_SENTINEL="/run/soperator-user-isolation.ready"

log() {
    logger -t soperator-user-isolation "$1" 2>/dev/null || true
}

# Sessions are placed on open only; cgroups of closed sessions are left in
# place and reused on the user's next login.
[ "${PAM_TYPE:-}" = "open_session" ] || exit 0

# The entrypoint creates the sentinel only after verifying writable cgroup v2
# and enabling controllers for the users/ subtree. Its content is the container's
# resolved cgroup base — do not assume /sys/fs/cgroup: with a host cgroup
# namespace that is the node's root tree, not ours.
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

# Skip root and system users: the operator's own SSH access and system
# services must never be resource-limited.
[ "${uid}" -ge 1000 ] || exit 0

cg="${cgroup_base}/users/user-${uid}"

if ! mkdir -p "${cg}" 2>/dev/null; then
    log "cannot create cgroup for uid ${uid}; session left unrestricted"
    exit 0
fi

# The cgroup files must stay root-owned: the cgroup tree is bind-mounted into
# the jail, and delegating ownership would let users lift their own limits.
if [ -n "${SOPERATOR_USER_ISOLATION_MEMORY_HIGH:-}" ]; then
    echo "${SOPERATOR_USER_ISOLATION_MEMORY_HIGH}" > "${cg}/memory.high" 2>/dev/null \
        || log "failed to set memory.high for uid ${uid}"
fi
if [ -n "${SOPERATOR_USER_ISOLATION_MEMORY_MAX:-}" ]; then
    echo "${SOPERATOR_USER_ISOLATION_MEMORY_MAX}" > "${cg}/memory.max" 2>/dev/null \
        || log "failed to set memory.max for uid ${uid}"
fi
if [ -n "${SOPERATOR_USER_ISOLATION_CPU_WEIGHT:-}" ]; then
    echo "${SOPERATOR_USER_ISOLATION_CPU_WEIGHT}" > "${cg}/cpu.weight" 2>/dev/null \
        || log "failed to set cpu.weight for uid ${uid}"
fi

# Move the session leader (our parent sshd session process) into the cgroup;
# the user's shell and everything spawned from it inherit the membership.
if ! echo "${PPID}" > "${cg}/cgroup.procs" 2>/dev/null; then
    log "failed to move session of uid ${uid} into ${cg}"
fi

exit 0
