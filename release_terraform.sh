#!/bin/bash

set -e

usage() { echo "usage: ${0} [-f] [-h]" >&2; exit 1; }

while getopts fh flag
do
    case "${flag}" in
        f) force=1;;
        h) usage;;
        *) usage;;
    esac
done

mkdir -p "terraform-releases"

read -r version < ./VERSION
version=$(echo "$version" | tr '.' '_' | tr '-' '_')

tarball="terraform-releases/unstable/slurm_operator_tf_${version}.tar.gz"
if [ ! -f "$tarball" ] || [ -n "$force" ]; then
    tar -czf "$tarball" \
        --exclude="terraform/.terraform" \
        --exclude="terraform/.terraform.lock.hcl" \
        --exclude="terraform/.terraform.tfstate.lock.info" \
        --exclude="terraform/terraform.tfstate" \
        --exclude="terraform/terraform.tfstate.backup" \
        terraform test
    echo "Created $(pwd)/$tarball"
fi
