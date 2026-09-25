#!/bin/bash

set -euo pipefail

metadata_file="${1:-/run/soperator/node_metadata.env}"
node_real_memory_bytes="${SOPERATOR_NODE_REAL_MEMORY_BYTES:-}"

if ! [[ "${node_real_memory_bytes}" =~ ^[0-9]+$ ]] || [[ "${node_real_memory_bytes}" == "0" ]]; then
    rm -f -- "${metadata_file}"
    echo "SOPERATOR_NODE_REAL_MEMORY_BYTES is unavailable or invalid; skipping node metadata export" >&2
    exit 0
fi

metadata_dir="$(dirname -- "${metadata_file}")"
install -d -m 0755 "${metadata_dir}"

temporary_file="$(mktemp "${metadata_file}.tmp.XXXXXX")"
trap 'rm -f -- "${temporary_file}"' EXIT

printf 'SOPERATOR_NODE_REAL_MEMORY_BYTES=%s\n' "${node_real_memory_bytes}" > "${temporary_file}"
chmod 0644 "${temporary_file}"
mv -f -- "${temporary_file}" "${metadata_file}"
trap - EXIT

echo "Exported node metadata to ${metadata_file}"
