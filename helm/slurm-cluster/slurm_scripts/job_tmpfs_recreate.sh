#!/bin/bash

set -euxo pipefail

echo "[$(date)] Create new or prune existing job tmpfs directory"

TMPFS_DIR="/mnt/jail/mnt/memory"

# Validate tmpfs directory
if [ ! -d "${TMPFS_DIR}" ]; then
    echo "${TMPFS_DIR} doesn't exist"
    exit 0
fi
if ! mountpoint -q "${TMPFS_DIR}"; then
    echo "${TMPFS_DIR} is not a mountpoint"
    exit 0
fi

if [ -z "${SLURM_JOB_ID}" ]; then
    echo "Slurm job id is not defined"
    exit 0
fi

JOB_TMPFS_DIR="${TMPFS_DIR}/job_${SLURM_JOB_ID}"

if [ -d "${JOB_TMPFS_DIR}" ]; then
    echo "Cleaning up existing job tmpfs directory (${JOB_TMPFS_DIR})"
    rm -rf -- "${JOB_TMPFS_DIR:?}"/..?* "${JOB_TMPFS_DIR:?}"/.[!.]* "${JOB_TMPFS_DIR:?}"/* || true
else
    echo "Creating job tmpfs directory ($JOB_TMPFS_DIR)"
    mkdir -p "${JOB_TMPFS_DIR:?}" || true
fi

# This is used when Enroot is configured to use tmpfs-backed data and runtime directories,
# i.e. when enroot.useDedicatedImageStorage is false.
ENROOT_TMPFS_DIR="${TMPFS_DIR}/enroot"

mkdir -p \
    "${ENROOT_TMPFS_DIR:?}/data" \
    "${ENROOT_TMPFS_DIR:?}/runtime"

chmod 1777 \
    "${ENROOT_TMPFS_DIR:?}" \
    "${ENROOT_TMPFS_DIR:?}/data" \
    "${ENROOT_TMPFS_DIR:?}/runtime"
