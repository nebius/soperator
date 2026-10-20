#!/bin/bash

set -euxo pipefail

DMESG_SINCE="${NVME_RAID_DMESG_SINCE:-15 minutes ago}"
MOUNT_POINT="${NVME_RAID_MOUNT_POINT:-}"
probe_file=""

cleanup_probe_file() {
    if [[ -n "${probe_file}" && -e "${probe_file}" ]]; then
        rm -f "${probe_file}"
    fi
}

discover_md_array() {
    local mount_point="$1"
    local source
    local md_name
    local md_dir

    if [[ ! -d "${mount_point}" ]]; then
        echo "NVMe RAID mount point ${mount_point} does not exist" >&3
        exit 1
    fi

    if ! source="$(findmnt -rn -T "${mount_point}" -o SOURCE 2>/dev/null)" || [[ -z "${source}" ]]; then
        echo "Could not determine the backing device for NVMe RAID mount point ${mount_point}" >&3
        exit 1
    fi

    md_array="${source%%\[*}"
    md_array="$(readlink -f "${md_array}" 2>/dev/null || printf '%s' "${md_array}")"
    md_name="${md_array##*/}"
    md_dir="/sys/block/${md_name}"

    if [[ ! -d "${md_dir}/md" ]]; then
        echo "NVMe RAID mount point ${mount_point} is backed by ${source}, not a RAID array" >&3
        exit 1
    fi

    if ! find "${md_dir}/slaves" -mindepth 1 -maxdepth 1 -printf '%f\n' 2>/dev/null | grep -q '^nvme'; then
        echo "RAID array ${md_array} backing ${mount_point} has no NVMe members" >&3
        exit 1
    fi
}

check_dmesg() {
    local dmesg_out
    local error_lines
    local pattern='nvme[^:[:space:]]*: I/O error while writing superblock|nvme[^:[:space:]]*: Remounting filesystem read-only|Buffer I/O error on dev nvme[[:alnum:]]+|blk_update_request: I/O error, dev nvme[[:alnum:]]+'

    if ! dmesg_out="$(dmesg --since "${DMESG_SINCE}" --color=never 2>/dev/null)"; then
        echo "Could not read dmesg for NVMe RAID check, skipping dmesg probe"
        return 0
    fi

    error_lines="$(printf '%s\n' "${dmesg_out}" | grep -E "${pattern}" || true)"
    if [[ -n "${error_lines}" ]]; then
        echo "Recent NVMe-related dmesg errors detected since ${DMESG_SINCE}: ${error_lines//$'\n'/ | }" >&3
        exit 1
    fi
}

check_md_array() {
    local md_array="$1"
    local md_name="${md_array##*/}"
    local md_dir="/sys/block/${md_name}/md"
    local array_state=""
    local sync_action=""
    local degraded=""

    if [[ ! -d "${md_dir}" ]]; then
        echo "Could not inspect RAID array ${md_array}: missing ${md_dir}" >&3
        exit 1
    fi

    if [[ -r "${md_dir}/array_state" ]]; then
        array_state="$(<"${md_dir}/array_state")"
    fi
    if [[ -r "${md_dir}/sync_action" ]]; then
        sync_action="$(<"${md_dir}/sync_action")"
    fi
    if [[ -r "${md_dir}/degraded" ]]; then
        degraded="$(<"${md_dir}/degraded")"
    fi

    if [[ -n "${degraded}" && "${degraded}" != "0" ]]; then
        echo "RAID array ${md_array} is degraded: missing ${degraded} device(s)" >&3
        exit 1
    fi

    if [[ -n "${array_state}" && "${array_state}" =~ ^(clear|inactive|suspended|readonly)$ ]]; then
        echo "RAID array ${md_array} is not healthy: array_state=${array_state}" >&3
        exit 1
    fi

    if [[ -n "${sync_action}" && ! "${sync_action}" =~ ^(idle|check)$ ]]; then
        echo "RAID array ${md_array} is not healthy: sync_action=${sync_action}" >&3
        exit 1
    fi
}

check_mount_rw() {
    local mount_point="$1"
    local expected
    local actual

    if [[ ! -d "${mount_point}" ]]; then
        echo "Mount point ${mount_point} does not exist" >&3
        exit 1
    fi

    if ! ls -ld "${mount_point}" >/dev/null 2>&1; then
        echo "Mount point ${mount_point} is not readable" >&3
        exit 1
    fi

    probe_file="${mount_point}/.nvme-raid-healthcheck.$$.$RANDOM"
    expected="nvme-raid-healthcheck-${SLURMD_NODENAME:-unknown}-$$-$RANDOM"

    if ! printf '%s\n' "${expected}" >"${probe_file}" 2>/dev/null; then
        echo "Mount point ${mount_point} is not writable" >&3
        exit 1
    fi

    if ! actual="$(cat "${probe_file}" 2>/dev/null)"; then
        echo "Mount point ${mount_point} is not readable after write" >&3
        exit 1
    fi

    rm -f "${probe_file}"
    probe_file=""

    if [[ "${actual}" != "${expected}" ]]; then
        echo "Mount point ${mount_point} returned unexpected data during read/write probe" >&3
        exit 1
    fi
}

trap cleanup_probe_file EXIT

echo "[$(date)] Checking NVMe RAID health"

if [[ -z "${MOUNT_POINT}" ]]; then
    echo "NVME_RAID_MOUNT_POINT is not set, skipping NVMe RAID health check"
    exit 0
fi

discover_md_array "${MOUNT_POINT}"
echo "NVMe RAID mount point ${MOUNT_POINT} is backed by ${md_array}"

check_md_array "${md_array}"
check_mount_rw "${MOUNT_POINT}"
check_dmesg

echo "NVMe RAID health check passed"
exit 0
