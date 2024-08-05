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

echo "Building worker_slurmd"
./build.sh -d docker/worker/slurmd.dockerfile                 -t worker_slurmd        ${iopt} ${nopt} > outputs/worker_slurmd.log        2>&1 &
echo "worker_slurmd building finished 1/5"
echo "Building controller_slurmctld"
./build.sh -d docker/controller/slurmctld.dockerfile          -t controller_slurmctld ${iopt} ${nopt} > outputs/controller_slurmctld.log 2>&1 &
echo "controller_slurmctld building finished 2/5"
echo "Building login_sshd"
./build.sh -d docker/login/sshd.dockerfile                    -t login_sshd           ${iopt} ${nopt} > outputs/login_sshd.log           2>&1 &
echo "login_sshd building finished 3/5"
echo "Building munge"
./build.sh -d docker/munge/munge.dockerfile                   -t munge                ${iopt} ${nopt} > outputs/munge.log                2>&1 &
echo "munge building finished 4/5"
echo "Building nccl_benchmark"
./build.sh -d docker/nccl_benchmark/nccl_benchmark.dockerfile -t nccl_benchmark       ${iopt} ${nopt} > outputs/nccl_benchmark.log       2>&1 &
echo "nccl_benchmark building finished 5/5"

wait

echo "Finished: build_all.sh"
