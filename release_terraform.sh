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

mkdir -p "terraform-releases/oldbius"

read -r version < ./VERSION
version=$(echo "$version" | tr '.' '_' | tr '-' '_')

tarball="terraform-releases/oldbius/unstable/slurm_operator_tf_${version}.tar.gz"
if [ ! -f "$tarball" ] || [ -n "$force" ]; then
    tar -czf "$tarball" \
        --exclude="terraform/oldbius/.terraform" \
        --exclude="terraform/oldbius/.terraform.lock.hcl" \
        --exclude="terraform/oldbius/.terraform.tfstate.lock.info" \
        --exclude="terraform/oldbius/terraform.tfstate" \
        --exclude="terraform/oldbius/terraform.tfstate.backup" \
        terraform test
    echo "Created $(pwd)/$tarball"
fi
