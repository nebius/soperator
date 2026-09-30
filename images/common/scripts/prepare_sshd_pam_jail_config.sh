#!/bin/bash

set -euo pipefail

source_config_dir="${1:-/mnt/ssh-configs}"
effective_config_dir="${2:?effective SSHD configuration directory is required}"

# Old images require ChrootDirectory during a rolling upgrade. PAM-jail images
# remove it from their private runtime snapshot before starting sshd.
echo "Prepare SSHD configuration for PAM jail"
cp -aL "${source_config_dir}/." "${effective_config_dir}/"

mapfile -d '' config_files < <(find "${effective_config_dir}" -type f -print0)
for config in "${config_files[@]}"; do
    if ! grep -IiqE '^[[:space:]]*chrootdirectory([[:space:]]|=)' "${config}"; then
        continue
    fi
    temporary_config=$(mktemp "${config}.XXXXXX")
    awk '
        {
            keyword = $1
            sub(/=.*/, "", keyword)
            if (tolower(keyword) != "chrootdirectory") {
                print
            }
        }
    ' "${config}" > "${temporary_config}"
    chmod --reference="${config}" "${temporary_config}"
    chown --reference="${config}" "${temporary_config}"
    mv "${temporary_config}" "${config}"
done

if grep -RIiqE '^[[:space:]]*chrootdirectory([[:space:]]|=)' "${effective_config_dir}"; then
    echo "SSHD configuration preparation: ChrootDirectory remains in the runtime snapshot" >&2
    exit 1
fi
