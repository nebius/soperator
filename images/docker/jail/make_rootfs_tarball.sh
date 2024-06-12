#!/bin/bash

set -e

# Build image from a dockerfile (-d) and save its rootfs into a tarball (-t)

usage() { echo "usage: ${0} -d <path_to_dockerfile> -t <path_to_tarball_file> [-i] [-h]" >&2; exit 1; }

while getopts d:t:ih flag
do
    case "${flag}" in
        d) dockerfile=${OPTARG};;
        t) tarball=${OPTARG};;
        i) ignore_cache=--no-cache;;
        h) usage;;
        *) usage;;
    esac
done

if [ -z "$dockerfile" ] || [ -z "$tarball" ]; then
    usage
fi

echo "Building ${tarball} from docker file ${dockerfile}"
docker build --tag jail --target jail --load $ignore_cache --platform=linux/amd64 -f "${dockerfile}" --output - . | pigz -p $(nproc) > "${tarball}"
echo "Built ${tarball} from docker file ${dockerfile}"
