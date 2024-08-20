#!/bin/bash

set -e

usage() { echo "usage: ${0} [-i] [-n] [-h]" >&2; exit 1; }

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

IMAGE_VERSION=${IMAGE_VERSION} ./build.sh -d worker/slurmd.dockerfile                 -t worker_slurmd        ${iopt} ${nopt}
IMAGE_VERSION=${IMAGE_VERSION} ./build.sh -d controller/slurmctld.dockerfile          -t controller_slurmctld ${iopt} ${nopt}
IMAGE_VERSION=${IMAGE_VERSION} ./build.sh -d login/sshd.dockerfile                    -t login_sshd           ${iopt} ${nopt}
IMAGE_VERSION=${IMAGE_VERSION} ./build.sh -d munge/munge.dockerfile                   -t munge                ${iopt} ${nopt}
IMAGE_VERSION=${IMAGE_VERSION} ./build.sh -d nccl_benchmark/nccl_benchmark.dockerfile -t nccl_benchmark       ${iopt} ${nopt}

wait

echo "Finished: build_all.sh"
