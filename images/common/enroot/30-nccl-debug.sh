#!/bin/bash
set -euo pipefail

state_dir="${_SNCCLD_ENROOT_STATE_DIR:-}"
log_dir="${_SNCCLD_ENROOT_LOG_DIR:-}"
[[ -n "$state_dir" ]] || exit 0

append_mount() {
    local directory="$1"

    # Fstab fields use octal escapes rather than shell quoting.
    directory="${directory//\\/\\134}"
    directory="${directory// /\\040}"
    directory="${directory//$'\t'/\\011}"
    directory="${directory//$'\n'/\\012}"

    printf '%s %s none x-create=dir,bind,rw,nosuid\n' \
        "$directory" "$directory" >> "${ENROOT_MOUNTS:?}"
}

append_mount "$state_dir"
if [[ -n "$log_dir" && "$log_dir" != "$state_dir" ]]; then
    append_mount "$log_dir"
fi
