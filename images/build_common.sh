#!/bin/bash

usage() { echo "usage: ${0} -v <version> [-i] [-h]" >&2; exit 1; }

while getopts v:ih flag
do
    case "${flag}" in
        v) version=${OPTARG};;
        i) iopt=-i;;
        h) usage;;
        *) usage;;
    esac
done

if [ -z "${version}" ]; then
    usage
fi

mkdir -p outputs

./build.sh -d docker/common/nccl.dockerfile       -t nccl       -v "${version}" ${iopt} -n > outputs/nccl.log       2>&1 &
./build.sh -d docker/common/nccl_tests.dockerfile -t nccl_tests -v "${version}" ${iopt} -n > outputs/nccl_tests.log 2>&1 &
./build.sh -d docker/common/slurm.dockerfile      -t slurm      -v "${version}" ${iopt} -n > outputs/slurm.log      2>&1 &

wait

echo "Finished: build_common.sh"
