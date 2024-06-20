#!/bin/bash

set -e

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

kubectl --context="$context_name" --namespace=slurm-poc apply -f common/local_pv_storageclass.yaml

kubectl --context="$context_name" --namespace=slurm-poc apply -f common/jail/pvc.yaml
kubectl --context="$context_name" --namespace=slurm-poc apply -f common/jail/pv.yaml

kubectl --context="$context_name" --namespace=slurm-poc apply -f node/controller/spool_pvc.yaml
kubectl --context="$context_name" --namespace=slurm-poc apply -f node/controller/spool_pv.yaml

kubectl --context="$context_name" --namespace=slurm-poc apply -f node/controller/spool_mount_daemonset.yaml
kubectl --context="$context_name" --namespace=slurm-poc apply -f common/jail/mount_daemonset.yaml
kubectl --context="$context_name" --namespace=slurm-poc rollout status daemonset jail-mount --timeout=10h

kubectl --context="$context_name" --namespace=slurm-poc apply -f common/config/munge_key_secret.yaml
kubectl --context="$context_name" --namespace=slurm-poc apply -f common/config/ssh_root_keys_secret.yaml
