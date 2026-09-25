#!/bin/bash

set -euo pipefail

source_file="${1:?source file is required}"
output_root="${2:?output root is required}"
module_dir="${output_root}/usr/lib/$(gcc -print-multiarch)/security"

mkdir -p "${module_dir}"
gcc \
    -std=c11 \
    -Wall \
    -Wextra \
    -Werror \
    -fPIC \
    -shared \
    -Wl,-z,defs \
    -Wl,-z,now \
    -Wl,-z,relro \
    -o "${module_dir}/pam_soperator_jail.so" \
    "${source_file}" \
    -lpam
chmod 0644 "${module_dir}/pam_soperator_jail.so"
