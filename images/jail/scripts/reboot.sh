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

if ! mountpoint -q /run/nvidia/driver; then
    echo "This command only works on GPU nodes"
    exit 1
fi

chroot /run/nvidia/driver nsenter -t 1 -m -u -i -n /usr/sbin/reboot
