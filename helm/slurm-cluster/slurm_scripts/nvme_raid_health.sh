#!/bin/bash

set -euxo pipefail

DMESG_SINCE="${NVME_RAID_DMESG_SINCE:-1 hour ago}"
probe_file=""

cleanup_probe_file() {
    if [[ -n "${probe_file}" && -e "${probe_file}" ]]; then
        rm -f "${probe_file}"
    fi
}

discover_nvme_disks() {
    mapfile -t nvme_disks < <(
        lsblk -dn -o NAME,TYPE 2>/dev/null | awk '$2 == "disk" && $1 ~ /^nvme/ { print "/dev/" $1 }'
    )
}

discover_md_arrays() {
    local md_sys md_name

    mapfile -t md_arrays < <(
        for md_sys in /sys/block/md*; do
            [[ -d "${md_sys}" ]] || continue
            md_name="${md_sys##*/}"
            if find "${md_sys}/slaves" -mindepth 1 -maxdepth 1 -printf '%f\n' 2>/dev/null | grep -q '^nvme'; then
                printf '/dev/%s\n' "${md_name}"
            fi
        done
    )
}

discover_mount_points() {
    local md_array
    local mounts=()

    mount_points=()

    for md_array in "${md_arrays[@]}"; do
        mapfile -t mounts < <(findmnt -rn -S "${md_array}" -o TARGET 2>/dev/null || true)
        if [[ ${#mounts[@]} -eq 0 ]]; then
            mapfile -t mounts < <(lsblk -nr -o MOUNTPOINT "${md_array}" 2>/dev/null | awk 'NF { print $0 }' || true)
        fi

        if [[ ${#mounts[@]} -eq 0 ]]; then
            echo "No mount point found for RAID array ${md_array}" >&3
            exit 1
        fi

        mount_points+=("${mounts[@]}")
    done

    mapfile -t mount_points < <(printf '%s\n' "${mount_points[@]}" | awk 'NF && !seen[$0]++')
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
    local detail

    if ! detail="$(mdadm --detail "${md_array}" 2>/dev/null)"; then
        echo "Could not inspect RAID array ${md_array} with mdadm" >&3
        exit 1
    fi

    if printf '%s\n' "${detail}" | grep -Eq 'State : .*(degraded|recovering|resyncing|failed|inactive)'; then
        echo "RAID array ${md_array} is not healthy: $(printf '%s\n' "${detail}" | awk -F' : ' '/State :/ { print $2; exit }')" >&3
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

discover_nvme_disks
if [[ ${#nvme_disks[@]} -eq 0 ]]; then
    echo "No NVMe disks detected, skipping"
    exit 0
fi
echo "Detected NVMe disks: ${nvme_disks[*]}"

discover_md_arrays
if [[ ${#md_arrays[@]} -eq 0 ]]; then
    echo "No NVMe-backed RAID arrays detected, skipping"
    exit 0
fi
echo "Detected NVMe-backed RAID arrays: ${md_arrays[*]}"

discover_mount_points
echo "Detected NVMe RAID mount points: ${mount_points[*]}"

check_dmesg

for md_array in "${md_arrays[@]}"; do
    check_md_array "${md_array}"
done

for mount_point in "${mount_points[@]}"; do
    check_mount_rw "${mount_point}"
done

echo "NVMe RAID health check passed"
exit 0
