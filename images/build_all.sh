#!/bin/bash

usage() { echo "usage: ${0} [-i] [-n] [-s] [-h]" >&2; exit 1; }

while getopts insh flag
do
    case "${flag}" in
        i) iopt=-i;;
        n) nopt=-n;;
        s) stableopt=-s;;
        h) usage;;
        *) usage;;
    esac
done

mkdir -p outputs

./build.sh -d docker/worker/slurmd.dockerfile                 -t worker_slurmd        ${iopt} ${nopt} ${stableopt} > outputs/worker_slurmd.log        2>&1 &
./build.sh -d docker/controller/slurmctld.dockerfile          -t controller_slurmctld ${iopt} ${nopt} ${stableopt} > outputs/controller_slurmctld.log 2>&1 &
./build.sh -d docker/login/sshd.dockerfile                    -t login_sshd           ${iopt} ${nopt} ${stableopt} > outputs/login_sshd.log           2>&1 &
./build.sh -d docker/munge/munge.dockerfile                   -t munge                ${iopt} ${nopt} ${stableopt} > outputs/munge.log                2>&1 &
./build.sh -d docker/nccl_benchmark/nccl_benchmark.dockerfile -t nccl_benchmark       ${iopt} ${nopt} ${stableopt} > outputs/nccl_benchmark.log       2>&1 &

wait

echo "Finished: build_all.sh"
