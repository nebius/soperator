#!/bin/bash

set -e

usage() { echo "usage: ${0} -d <description> [-f] [-h]" >&2; exit 1; }

while getopts d:fh flag
do
    case "${flag}" in
        d) description=${OPTARG};;
        f) force=1;;
        h) usage;;
        *) usage;;
    esac
done

if [ -z "$description" ]; then
    usage
fi

mkdir -p "terraform-releases"

description=$(echo "$description" | tr '[:upper:]' '[:lower:]' | tr -c '[:alnum:]' '_' | sed 's/_*$//')

read -r version < ./VERSION
version=$(echo "$version" | tr '.' '_')

tarball="terraform-releases/unstable/slurm_operator_tf_${version}_${description}.tar.gz"
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
