#!/bin/bash

usage() { echo "usage: ${0} -v <version> [-i] [-n] [-s] [-h]" >&2; exit 1; }

while getopts v:insh flag
do
    case "${flag}" in
        v) version=${OPTARG};;
        i) iopt=-i;;
        n) nopt=-n;;
        s) stableopt=-s;;
        h) usage;;
        *) usage;;
    esac
done

if [ -z "${version}" ]; then
    usage
fi

mkdir -p outputs

./build.sh -d docker/worker/slurmd.dockerfile                 -t worker_slurmd        -v "${version}" ${iopt} ${nopt} ${stableopt} > outputs/worker_slurmd.log        2>&1 &
./build.sh -d docker/controller/slurmctld.dockerfile          -t controller_slurmctld -v "${version}" ${iopt} ${nopt} ${stableopt} > outputs/controller_slurmctld.log 2>&1 &
./build.sh -d docker/login/sshd.dockerfile                    -t login_sshd           -v "${version}" ${iopt} ${nopt} ${stableopt} > outputs/login_sshd.log           2>&1 &
./build.sh -d docker/munge/munge.dockerfile                   -t munge                -v "${version}" ${iopt} ${nopt} ${stableopt} > outputs/munge.log                2>&1 &
./build.sh -d docker/nccl_benchmark/nccl_benchmark.dockerfile -t nccl_benchmark       -v "${version}" ${iopt} ${nopt} ${stableopt} > outputs/nccl_benchmark.log       2>&1 &

wait

echo "Finished: build_all.sh"
