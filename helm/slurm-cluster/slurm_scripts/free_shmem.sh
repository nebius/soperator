#!/bin/bash

set -euxo pipefail

shmem_dir="/mnt/jail/mnt/memory"

if [ ! -d "$shmem_dir" ]; then
    echo "$shmem_dir doesn't exist"
    exit 0
fi

if ! mountpoint -q "$shmem_dir"; then
    echo "$shmem_dir is not a mountpoint"
    exit 0
fi

rm -rf -- "${shmem_dir:?}"/..?* "${shmem_dir:?}"/.[!.]* "${shmem_dir:?}"/* || true
