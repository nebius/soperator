#!/bin/bash

set -e

usage() { echo "usage: ${0} -u <ssh_user> -k <path_to_ssh_key> -a <address_of_build_agent> [-h]" >&2; exit 1; }

while getopts u:k:a:h flag
do
    case "${flag}" in
        u) user=${OPTARG};;
        k) key=${OPTARG};;
        a) address=${OPTARG};;
        h) usage;;
        *) usage;;
    esac
done

if [ -z "$user" ] || [ -z "$key" ] || [ -z "$address" ]; then
    usage
fi

echo "Uploading sources to the slurm-build-agent VM (https://console.nebius.ai/folders/bje82q7sm8njm3c4rrlq/compute/instance/dp75k0v9ooje2g6vk0c0/overview)"

rsync -Prv \
    -e "ssh -i ${key}" \
    --exclude '.DS_Store' \
    --exclude '.idea' \
    --exclude '.github' \
    --exclude '.git' \
    --exclude 'bin' \
    --exclude 'terraform' \
    --exclude 'test' \
    ./ "${user}"@"${address}":/usr/src/prototypes/slurm/${user}/
