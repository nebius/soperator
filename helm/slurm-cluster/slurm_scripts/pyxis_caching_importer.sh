#!/bin/bash
# Patched version of https://github.com/NVIDIA/pyxis/blob/v0.23.0/importers/caching_importer.sh

set -euo pipefail

readonly cmd="$1"

readonly cache_dir="${ENROOT_CONTAINER_IMAGES_CACHE_DIR:-/var/cache/enroot-container-images}"
readonly node_id="${SLURMD_NODENAME:-${HOSTNAME:-unknown}}"
readonly squashfs_temp_path="${cache_dir}/${SLURM_JOB_ID}.${SLURM_STEP_ID}.${node_id}.sqsh"

# Since it's not an ephemeral squashfs file, we can use compression.
export ENROOT_SQUASH_OPTIONS="-comp zstd -Xcompression-level 3 -b 1M"

publish_image()
{
    # The cache may have been populated after the lock was requested.
    if [ -e "${squashfs_path}" ]; then
        return
    fi

    # Each node owns a separate temporary path, so cleanup from one node cannot
    # remove another node's in-progress import.
    rm -f "${squashfs_temp_path}"
    trap 'rm -f "${squashfs_temp_path}"' EXIT

    # TODO: use `digest` approach once 406 Not Acceptable is tolerated in enroot
    # https://github.com/NVIDIA/enroot/pull/263
    # if [[ "${image_uri}" == *"@${digest}" ]]; then
    #     # URI already has the digest in it.
    #     enroot import --output "${squashfs_temp_path}" "${image_uri}" >&2
    # else
    #     # Add the digest to the URI.
    #     enroot import --output "${squashfs_temp_path}" "${image_uri}@${digest}" >&2
    # fi
    enroot import --output "${squashfs_temp_path}" "${image_uri}" >&2

    # Save the URI as an extended attribute.
    if command -v "setfattr" >/dev/null; then
        setfattr -n user.image_uri -v "${image_uri}" "${squashfs_temp_path}"
    fi

    chmod 777 "${squashfs_temp_path}"

    # The temporary and final paths are in the same directory, so rename
    # publishes a complete SquashFS image atomically.
    mv -n "${squashfs_temp_path}" "${squashfs_path}"

    if [ ! -e "${squashfs_path}" ]; then
        echo "error: could not publish image cache: ${squashfs_path}" >&2
        exit 1
    fi

    # mv -n can leave the source in place if an external writer published the
    # destination without following this locking protocol.
    rm -f "${squashfs_temp_path}"
    trap - EXIT
}

case "${cmd}" in
    get)
        if [ $# -ne 2 ]; then
            echo "usage: $0 get URI" >&2
            exit 1
        fi

        readonly image_uri="$2"

        mkdir -p -m 700 "${cache_dir}"

        readonly digest=$(enroot digest "${image_uri}")
        if [ -z "${digest}" ]; then
            echo "error: could not retrieve digest for image: ${image_uri}" >&2
            exit 1
        fi
        readonly squashfs_path="${cache_dir}/${digest}.sqsh"
        readonly lock_path="${squashfs_path}.lock"

        # Warm-cache fast path: completed images are immutable and can be read
        # concurrently, so readers do not need to join the exclusive lock queue.
        if [ -e "${squashfs_path}" ]; then
            echo "${squashfs_path}"
            exit 0
        fi

        # Use a stable, digest-scoped lock. Do not unlink it: flock locks the
        # opened inode, and unlinking it can let callers lock different inodes.
        # The cache directory is shared between Slurm users, so lock files must
        # be writable by callers other than their creator.
        readonly previous_umask=$(umask)
        umask 000
        exec 9>>"${lock_path}"
        umask "${previous_umask}"

        if flock -n -x 9; then
            # This caller is the cold-cache producer.
            publish_image
        else
            # A producer is already active. Shared waiters are released
            # together when the producer closes its exclusive lock.
            flock -s 9

            if [ ! -e "${squashfs_path}" ]; then
                # The producer exited without publishing. Queue for exclusive
                # ownership so one waiter retries while the others remain safe.
                flock -u 9
                flock -x 9
                publish_image
            fi
        fi

        # Output the squashfs path on stdout for pyxis to read
        echo "${squashfs_path}"
        ;;
    release)
        if [ $# -ne 1 ]; then
            echo "usage: $0 release" >&2
            exit 1
        fi

        # Temporary paths are node-specific, so releases do not need to be
        # serialized and cannot remove another node's active import.
        rm -f "${squashfs_temp_path}"
        ;;
    *)
        echo "error: unknown command: ${cmd}" >&2
        exit 1
        ;;
esac
