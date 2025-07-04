#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code

usage() {
    echo "Get NVIDIA bug report from a worker node." >&2
    echo "" >&2
    echo "usage: ${0} [-w worker_name] [-i instance_id] [-h]" >&2
    echo "       (either -w or -i must be set)"
    exit 1
}

while getopts w:i:h flag
do
    case "${flag}" in
        w) worker_name=${OPTARG};;
        i) instance_id=${OPTARG};;
        h) usage;;
        *) usage;;
    esac
done

if [ -z "$worker_name" ] && [ -z "$instance_id" ]; then
    usage
fi

if [ -z "$worker_name" ] && [ -n "$instance_id" ]; then
    worker_name=$(scontrol show nodes --json | jq -r ".nodes[] | select(.instance_id == \"${instance_id}\") | .name")
    if [ -z "$worker_name" ]; then
        echo "Error: No node matches the provided instance_id '${instance_id}'." >&2
        exit 1
    fi
fi

ssh -t "${worker_name}" bash -s <<'EOF'
  sudo chroot /run/nvidia/driver /usr/bin/nvidia-bug-report.sh
  sudo mv /run/nvidia/driver/nvidia-bug-report.log.gz /tmp/
EOF

scp "${worker_name}":/tmp/nvidia-bug-report.log.gz "$(pwd)/${worker_name}-nvidia-bug-report.log.gz"
