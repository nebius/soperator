#!/bin/bash

set -euo pipefail

jail_dir="${1:-/mnt/jail}"
runtime_dir="${2:-/run/soperator-login-warning}"
script_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

# The jail persists across upgrades. Mount a pod-local copy so its existing
# Bash configuration is preserved without changing other login pods or workers.
install -d -m 0755 "${runtime_dir}"
{
    cat "${jail_dir}/etc/bash.bashrc"
    printf '\n'
    cat "${script_dir}/login_warning.sh"
} > "${runtime_dir}/bash.bashrc"
chmod 0644 "${runtime_dir}/bash.bashrc"
mount --bind "${runtime_dir}/bash.bashrc" "${jail_dir}/etc/bash.bashrc"
mount -o remount,bind,ro "${jail_dir}/etc/bash.bashrc"
