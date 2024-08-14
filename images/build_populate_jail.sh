#!/bin/bash

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

./jail/make_rootfs_tarball.sh -d jail/jail.dockerfile ${iopt}
IMAGE_VERSION=${IMAGE_VERSION} ./build.sh -d populate_jail/populate_jail.dockerfile -t populate_jail ${iopt} ${nopt}

echo "Finished: build_populate_jail.sh"
