#!/bin/bash

set -e

usage() { echo "usage: ${0} -v <version> [-f] [-h]" >&2; exit 1; }

while getopts v:f:h flag
do
    case "${flag}" in
        v) version=${OPTARG};;
        f) force=${OPTARG};;
        h) usage;;
        *) usage;;
    esac
done

if [ -z "$version" ]; then
    usage
fi

mkdir -p "terraform-releases"

tarball="terraform-releases/slurm_operator_tf_$version.tar.gz"
if [ ! -f "$tarball" ] || [ -n "$force" ]; then
    tar -czf "$tarball" \
        --exclude="terraform/slurm-cluster/.terraform" \
        --exclude="terraform/slurm-cluster/.terraform.lock.hcl" \
        --exclude="terraform/slurm-cluster/.terraform.tfstate.lock.info" \
        --exclude="terraform/slurm-cluster/terraform.tfstate" \
        --exclude="terraform/slurm-cluster/terraform.tfstate.backup" \
        terraform helm test
    echo "Created $(pwd)/$tarball"
fi
