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

echo "Preparing jail dir"
mkdir -p jail
rm -rf jail/* image.tar

echo "Building tarball from docker file ${dockerfile}"
docker build --tag jail --target jail --load $ignore_cache --platform=linux/amd64 -f "${dockerfile}" --output type=tar,dest=image.tar .

echo "Unpack tarball"
tar -xvf image.tar -C jail/
