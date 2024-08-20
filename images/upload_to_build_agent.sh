#!/bin/bash

set -e

usage() { echo "usage: ${0} -u <ssh_user> -k <path_to_ssh_key> [-h]" >&2; exit 1; }

while getopts u:k:h flag
do
    case "${flag}" in
        u) user=${OPTARG};;
        k) key=${OPTARG};;
        h) usage;;
        *) usage;;
    esac
done

if [ -z "$user" ] || [ -z "$key" ]; then
    usage
fi

echo "Uploading sources to the slurm-build-agent VM (https://console.nebius.ai/folders/bje82q7sm8njm3c4rrlq/compute/instance/dp75k0v9ooje2g6vk0c0/overview)"

agent_ip=195.242.25.163

rsync -Prv -e "ssh -i ${key}" images/                 "${user}"@"${agent_ip}":/usr/src/prototypes/slurm/${user}/
rsync -Prv -e "ssh -i ${key}" VERSION             "${user}"@"${agent_ip}":/usr/src/prototypes/slurm/${user}/
