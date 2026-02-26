#!/bin/bash
# Patched version of https://github.com/NVIDIA/pyxis/blob/v0.23.0/importers/caching_importer.sh

set -euo pipefail

readonly cmd="$1"

readonly cache_dir="${ENROOT_CONTAINER_IMAGES_CACHE_DIR:-/var/cache/enroot-container-images}"
readonly squashfs_temp_path="${cache_dir}/${SLURM_JOB_ID}.${SLURM_STEP_ID}.sqsh"

# Since it's not an ephemeral squashfs file, we can use compression.
export ENROOT_SQUASH_OPTIONS="-comp zstd -Xcompression-level 3 -b 1M"

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

        if [ ! -e "${squashfs_path}" ]; then
            # TODO: use `digest` approach once 406 Not Acceptable is tolerated in enroot
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
            mv -n "${squashfs_temp_path}" "${squashfs_path}"
        fi

        # Output the squashfs path on stdout for pyxis to read
        echo "${squashfs_path}"
        ;;
    release)
        if [ $# -ne 1 ]; then
            echo "usage: $0 release" >&2
            exit 1
        fi

        # Remove temporary file if still present (e.g. "get" was interrupted)
        rm -f "${squashfs_temp_path}"
        ;;
    *)
        echo "error: unknown command: ${cmd}" >&2
        exit 1
        ;;
esac
