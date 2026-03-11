#!/bin/bash

set -euo pipefail

log() {
  echo "[$(date -u '+%Y-%m-%d %H:%M:%S UTC')] $*"
}

declare -a errors=()
declare -a nvme_disks=()
declare -a md_arrays=()
declare -a mount_points=()

append_error() {
  errors+=("$1")
}

join_by() {
  local delimiter="$1"
  shift
  local first=1
  for item in "$@"; do
    if [[ $first -eq 1 ]]; then
      printf '%s' "$item"
      first=0
    else
      printf '%s%s' "$delimiter" "$item"
    fi
  done
}

discover_nvme_disks() {
  mapfile -t nvme_disks < <(
    lsblk -dn -o NAME,TYPE 2>/dev/null | awk '$2 == "disk" && $1 ~ /^nvme/ { print "/dev/" $1 }'
  )
}

discover_md_arrays() {
  local md_sys md_name
  for md_sys in /sys/block/md*; do
    [[ -d "$md_sys" ]] || continue
    md_name="${md_sys##*/}"
    if find "$md_sys/slaves" -mindepth 1 -maxdepth 1 -printf '%f\n' 2>/dev/null | grep -q '^nvme'; then
      md_arrays+=("/dev/$md_name")
    fi
  done
}

discover_mount_points() {
  local md_array
  local mounts=()

  for md_array in "${md_arrays[@]}"; do
    mapfile -t mounts < <(findmnt -rn -S "$md_array" -o TARGET 2>/dev/null || true)
    if [[ ${#mounts[@]} -eq 0 ]]; then
      mapfile -t mounts < <(lsblk -nr -o MOUNTPOINT "$md_array" 2>/dev/null | awk 'NF { print $0 }' || true)
    fi
    if [[ ${#mounts[@]} -eq 0 ]]; then
      append_error "no mount point found for RAID array $md_array"
      continue
    fi
    mount_points+=("${mounts[@]}")
  done

  if [[ ${#mount_points[@]} -gt 0 ]]; then
    mapfile -t mount_points < <(printf '%s\n' "${mount_points[@]}" | awk 'NF && !seen[$0]++')
  fi
}

check_dmesg() {
  local dmesg_out error_lines=()
  local pattern='nvme[^:[:space:]]*: I/O error while writing superblock|nvme[^:[:space:]]*: Remounting filesystem read-only|Buffer I/O error on dev nvme[[:alnum:]]+|blk_update_request: I/O error, dev nvme[[:alnum:]]+'

  if ! dmesg_out="$(dmesg --color=never 2>/dev/null)"; then
    append_error "failed to read dmesg"
    return
  fi

  mapfile -t error_lines < <(printf '%s\n' "$dmesg_out" | grep -E "$pattern" || true)
  if [[ ${#error_lines[@]} -gt 0 ]]; then
    append_error "NVMe-related dmesg errors detected: $(join_by ' | ' "${error_lines[@]}")"
  fi
}

check_mount_rw() {
  local mount_point="$1"
  local probe_file expected actual

  if [[ ! -d "$mount_point" ]]; then
    append_error "mount point $mount_point does not exist"
    return
  fi

  if ! ls -ld "$mount_point" >/dev/null 2>&1; then
    append_error "mount point $mount_point is not readable"
    return
  fi

  probe_file="$mount_point/.nvme-raid-healthcheck.$$.$RANDOM"
  expected="nvme-raid-healthcheck-$SLURMD_NODENAME-$$-$RANDOM"

  if ! printf '%s\n' "$expected" >"$probe_file" 2>/dev/null; then
    append_error "mount point $mount_point is not writable"
    return
  fi

  if ! actual="$(cat "$probe_file" 2>/dev/null)"; then
    rm -f "$probe_file"
    append_error "mount point $mount_point is not readable after write"
    return
  fi

  rm -f "$probe_file"

  if [[ "$actual" != "$expected" ]]; then
    append_error "mount point $mount_point returned unexpected data during read/write probe"
  fi
}

log "Checking NVMe RAID health"

discover_nvme_disks
if [[ ${#nvme_disks[@]} -eq 0 ]]; then
  log "No NVMe disks detected, skipping"
  exit 0
fi
log "Detected NVMe disks: $(join_by ', ' "${nvme_disks[@]}")"

discover_md_arrays
if [[ ${#md_arrays[@]} -eq 0 ]]; then
  append_error "no RAID array found that uses NVMe devices"
else
  log "Detected NVMe-backed RAID arrays: $(join_by ', ' "${md_arrays[@]}")"
fi

discover_mount_points
if [[ ${#mount_points[@]} -gt 0 ]]; then
  log "Detected NVMe RAID mount points: $(join_by ', ' "${mount_points[@]}")"
fi

check_dmesg

for mount_point in "${mount_points[@]}"; do
  check_mount_rw "$mount_point"
done

if [[ ${#errors[@]} -gt 0 ]]; then
  printf '%s\n' "$(join_by '; ' "${errors[@]}")" >&3
  exit 1
fi

log "NVMe RAID health check passed"
exit 0
