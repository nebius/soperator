#!/bin/bash

usage() { echo "usage: ${0} [-i][-n][-h]" >&2; exit 1; }

while getopts inh flag
do
    case "${flag}" in
        i) iopt=-i;;
        n) nopt=-n;;
        h) usage;;
        *) usage;;
    esac
done

mkdir -p outputs

./build.sh -t worker_slurmd        -d docker/worker/slurmd.dockerfile                 $iopt $nopt > outputs/worker_slurmd.log        2>&1 &
./build.sh -t controller_slurmctld -d docker/controller/slurmctld.dockerfile          $iopt $nopt > outputs/controller_slurmctld.log 2>&1 &
./build.sh -t login_sshd           -d docker/login/sshd.dockerfile                    $iopt $nopt > outputs/login_sshd.log           2>&1 &
./build.sh -t munge                -d docker/munge/munge.dockerfile                   $iopt $nopt > outputs/munge.log                2>&1 &
./build.sh -t nccl_benchmark       -d docker/nccl_benchmark/nccl_benchmark.dockerfile $iopt $nopt > outputs/nccl_benchmark.log       2>&1 &

wait

echo "Finished: build_all.sh"
