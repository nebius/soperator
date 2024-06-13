#!/bin/bash

usage() { echo "usage: ${0} [-i][-h]" >&2; exit 1; }

while getopts ih flag
do
    case "${flag}" in
        i) iopt=-i;;
        h) usage;;
        *) usage;;
    esac
done

mkdir -p outputs

./build.sh -t nccl       -d docker/common/nccl.dockerfile       $iopt -n > outputs/nccl.log       2>&1 &
./build.sh -t nccl_tests -d docker/common/nccl_tests.dockerfile $iopt -n > outputs/nccl_tests.log 2>&1 &
./build.sh -t slurm      -d docker/common/slurm.dockerfile      $iopt -n > outputs/slurm.log      2>&1 &

wait

echo "Finished: build_common.sh"
