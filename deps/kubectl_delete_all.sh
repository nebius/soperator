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

## Populating a jail takes a long now (~1h) so it's not recommended to repopulate it without a reason
#kubectl --context="$context_name" --namespace=dstaroff delete -f common/jail/populate_jail_job.yaml

kubectl --context="$context_name" --namespace=dstaroff delete -f common/jail/mount_daemonset.yaml
kubectl --context="$context_name" --namespace=dstaroff delete -f node/controller/spool_mount_daemonset.yaml

kubectl --context="$context_name" --namespace=dstaroff delete -f node/controller/spool_pv.yaml &
kubectl --context="$context_name" --namespace=dstaroff delete -f node/controller/spool_pvc.yaml

kubectl --context="$context_name" --namespace=dstaroff delete -f common/jail/pv.yaml &
kubectl --context="$context_name" --namespace=dstaroff delete -f common/jail/pvc.yaml
