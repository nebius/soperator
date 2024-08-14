#!/bin/bash

usage() { echo "usage: ${0} [-i] [-h]" >&2; exit 1; }

while getopts ish flag
do
    case "${flag}" in
        i) iopt=-i;;
        h) usage;;
        *) usage;;
    esac
done

mkdir -p outputs

IMAGE_VERSION=latest ./build.sh -d common/nccl.dockerfile       -t nccl       ${iopt} -n
IMAGE_VERSION=latest ./build.sh -d common/nccl_tests.dockerfile -t nccl_tests ${iopt} -n
IMAGE_VERSION=latest ./build.sh -d common/slurm.dockerfile      -t slurm      ${iopt} -n

wait

echo "Finished: build_common.sh"
