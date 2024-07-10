#!/bin/bash

usage() { echo "usage: ${0} [-i] [-h]" >&2; exit 1; }

while getopts ih flag
do
    case "${flag}" in
        i) iopt=-i;;
        h) usage;;
        *) usage;;
    esac
done

mkdir -p outputs

./build.sh -d docker/common/nccl.dockerfile       -t nccl       ${iopt} -n > outputs/nccl.log       2>&1 &
./build.sh -d docker/common/nccl_tests.dockerfile -t nccl_tests ${iopt} -n > outputs/nccl_tests.log 2>&1 &
./build.sh -d docker/common/slurm.dockerfile      -t slurm      ${iopt} -n > outputs/slurm.log      2>&1 &

wait

echo "Finished: build_common.sh"
