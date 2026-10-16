#!/bin/bash

set -euo pipefail

metadata_file="${1:-/run/soperator/node_metadata.env}"

metadata_dir="$(dirname -- "${metadata_file}")"
install -d -m 0755 "${metadata_dir}"

temporary_file="$(mktemp "${metadata_file}.tmp.XXXXXX")"
trap 'rm -f -- "${temporary_file}"' EXIT

# Skip variables that must not override the prolog/epilog environment of check_runner.py,
# and values that can't be stored as a single NAME=value line.
excluded_names='^(KUBERNETES_|HELM_|LC_|LS_|NVIDIA_|LD_|LESS|TZ$|DEBIAN_FRONTEND$|PATH$|SHLVL$|TERM$|LANG$|HOME$|PWD$|_$)'
while IFS= read -r name; do
    if [[ "${name}" =~ ${excluded_names} ]] || ! [[ "${name}" =~ ^[A-Za-z_][A-Za-z0-9_]*$ ]]; then
        continue
    fi
    value="${!name}"
    if [[ "${value}" == *$'\n'* || "${value}" == *$'\r'* ]]; then
        echo "Skipping multi-line variable ${name} in node metadata" >&2
        continue
    fi
    printf '%s=%s\n' "${name}" "${value}"
done < <(compgen -e) > "${temporary_file}"
chmod 0644 "${temporary_file}"
mv -f -- "${temporary_file}" "${metadata_file}"
trap - EXIT

echo "Exported node metadata to ${metadata_file}"
