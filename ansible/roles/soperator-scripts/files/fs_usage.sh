#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code

usage() {
    echo "Get Soperator filesystem usage." >&2
    echo "" >&2
    echo "usage: ${0} [-s] [-l] [-h]" >&2
    echo "  -s - only show shared filesystems"
    echo "  -l - only show local filesystems"
    echo "  -m - only show in-memory filesystems"
    exit 1
}

while getopts slmh flag
do
    case "${flag}" in
        s) only_shared="1";;
        l) only_local="1";;
        m) only_memory="1";;
        h) usage;;
        *) usage;;
    esac
done

types="virtiofs,tmpfs,nfs4,overlay,ext4"
if [ -n "$only_shared" ]; then
    types="virtiofs,nfs4"
elif [ -n "$only_local" ]; then
    types="overlay,ext4"
elif [ -n "$only_memory" ]; then
    types="tmpfs"
fi

findmnt -o SIZE,USE%,FSTYPE,TARGET --types "$types" \
    | grep -vE '/dev|/run|/sys|/etc|/usr|/opt|/proc|/var' \
    | sed 's/SIZE/Size/' | sed 's/USE%/Use%/' | sed 's/FSTYPE/FSType/' | sed 's/TARGET/Directory/' \
    | sed 's/^/  /'
