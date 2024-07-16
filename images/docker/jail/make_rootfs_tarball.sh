#!/bin/bash

set -e

# Build image from a dockerfile (-d) and save its rootfs into a tarball (-t)

usage() { echo "usage: ${0} -d <path_to_dockerfile> [-i] [-h]" >&2; exit 1; }

while getopts d:t:ih flag
do
    case "${flag}" in
        d) dockerfile=${OPTARG};;
        i) ignore_cache=--no-cache;;
        h) usage;;
        *) usage;;
    esac
done

if [ -z "$dockerfile" ]; then
    usage
fi

echo "Removing previous jail rootfs tar archive"
rm -rf jail_rootfs.tar

echo "Building tarball from docker file ${dockerfile}"
docker build --tag jail --target jail --load $ignore_cache --platform=linux/amd64 -f "${dockerfile}" --output type=tar,dest=jail_rootfs.tar .

echo "Built tarball jail_rootfs.tar"
echo "OK"
