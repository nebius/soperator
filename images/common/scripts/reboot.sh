#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code

usage() {
    echo "Restart this Slurm worker." >&2
    echo "" >&2
    echo "usage: ${0} [-h]" >&2
    exit 1
}

while getopts h flag
do
    case "${flag}" in
        h) usage;;
        *) usage;;
    esac
done

POD_NAME="$(hostname)"
NS="$(cat /var/run/secrets/kubernetes.io/serviceaccount/namespace)"
TOKEN="$(cat /var/run/secrets/kubernetes.io/serviceaccount/token)"
CACERT=/var/run/secrets/kubernetes.io/serviceaccount/ca.crt
POD_URL="https://kubernetes.default.svc/api/v1/namespaces/$NS/pods/$POD_NAME"

if POD_JSON="$(curl -fsS --cacert "$CACERT" \
    -H "Authorization: Bearer $TOKEN" \
    "$POD_URL")" && [[ "$POD_JSON" == *'"kekus/todelete":"true"'* ]]; then
    curl -fsS --cacert "$CACERT" \
        -H "Authorization: Bearer $TOKEN" \
        -H "Content-Type: application/merge-patch+json" \
        -X PATCH \
        -d '{"metadata":{"labels":{"kekus/todelete":"false"}}}' \
        "$POD_URL"
    curl -fsS --cacert "$CACERT" \
        -H "Authorization: Bearer $TOKEN" \
        -X DELETE \
        "$POD_URL"
else
    if ! mountpoint -q /run/nvidia/driver; then
        echo "This command only works on GPU nodes"
        exit 1
    fi
    chroot /run/nvidia/driver nsenter -t 1 -m -u -i -n /usr/sbin/reboot
fi
