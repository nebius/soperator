#!/bin/bash

set -euo pipefail

source_config="${1:-/mnt/ssh-configs/sshd_config}"
effective_config="${2:-/run/soperator-sshd_config}"

# Keep ChrootDirectory in the rendered config so older images remain jailed
# during a rolling upgrade. Images with the PAM module remove it at runtime.
echo "Prepare sshd config for PAM jail"
awk '
    {
        keyword = $1
        sub(/=.*/, "", keyword)
        if (tolower(keyword) != "chrootdirectory") {
            print
        }
    }
' "${source_config}" > "${effective_config}"
chmod 0600 "${effective_config}"

exec /usr/sbin/sshd -D -e -f "${effective_config}"
