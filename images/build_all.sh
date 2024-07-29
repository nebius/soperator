#!/bin/bash

usage() { echo "usage: ${0} [-i] [-n] [-h]" >&2; exit 1; }

while getopts insh flag
do
    case "${flag}" in
        i) iopt=-i;;
        n) nopt=-n;;
        h) usage;;
        *) usage;;
    esac
done

mkdir -p outputs

./build.sh -d docker/worker/slurmd.dockerfile                 -t worker_slurmd        ${iopt} ${nopt} > outputs/worker_slurmd.log        2>&1 &
./build.sh -d docker/controller/slurmctld.dockerfile          -t controller_slurmctld ${iopt} ${nopt} > outputs/controller_slurmctld.log 2>&1 &
./build.sh -d docker/login/sshd.dockerfile                    -t login_sshd           ${iopt} ${nopt} > outputs/login_sshd.log           2>&1 &
./build.sh -d docker/munge/munge.dockerfile                   -t munge                ${iopt} ${nopt} > outputs/munge.log                2>&1 &
./build.sh -d docker/nccl_benchmark/nccl_benchmark.dockerfile -t nccl_benchmark       ${iopt} ${nopt} > outputs/nccl_benchmark.log       2>&1 &

wait

echo "Finished: build_all.sh"
