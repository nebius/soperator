#!/bin/bash

usage() { echo "usage: ${0} -c <context_name> [-h]" >&2; exit 1; }

while getopts c:h flag
do
    case "${flag}" in
        c) context_name=${OPTARG};;
        h) usage;;
        *) usage;;
    esac
done

if [ -z "$context_name" ]; then
    usage
fi

kubectl --context="$context_name" --namespace=slurm-poc delete -f common/config/ssh_root_keys_secret.yaml
kubectl --context="$context_name" --namespace=slurm-poc delete -f common/config/munge_key_secret.yaml

kubectl --context="$context_name" --namespace=slurm-poc delete -f common/jail/mount_daemonset.yaml
kubectl --context="$context_name" --namespace=slurm-poc delete -f node/controller/spool_mount_daemonset.yaml

kubectl --context="$context_name" --namespace=slurm-poc delete -f node/controller/spool_pv.yaml &
kubectl --context="$context_name" --namespace=slurm-poc delete -f node/controller/spool_pvc.yaml

kubectl --context="$context_name" --namespace=slurm-poc delete -f common/jail/pv.yaml &
kubectl --context="$context_name" --namespace=slurm-poc delete -f common/jail/pvc.yaml
