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

./docker/jail/make_rootfs_tarball.sh -d docker/jail/jail.dockerfile ${iopt} > outputs/make_rootfs_tarball.log 2>&1
./build.sh -d docker/populate_jail/populate_jail.dockerfile -t populate_jail ${iopt} ${nopt} ${stableopt} > outputs/populate_jail.log 2>&1

echo "Finished: build_populate_jail.sh"
