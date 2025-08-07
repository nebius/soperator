#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code

usage() {
    echo "Get Soperator filesystem usage." >&2
    echo "" >&2
    echo "usage: ${0} [-s] [-l] [-h]" >&2
    echo "  -s - only show shared filesystems"
    echo "  -l - only show local filesystems"
    exit 1
}

while getopts slh flag
do
    case "${flag}" in
        s) only_shared=1;;
        l) only_local=1;;
        h) usage;;
        *) usage;;
    esac
done

types="virtiofs,tmpfs,nfs4,overlay,ext4"
if [[ $only_shared -eq 1 ]]; then
    types="virtiofs,nfs4"
elif [[ $only_local -eq 1 ]]; then
    types="tmpfs,overlay,ext4"
fi

findmnt -o SIZE,USE%,FSTYPE,TARGET --types "$types" \
    | grep -vE '/dev|/run|/sys|/etc|/usr|/opt|/proc|/var' \
    | sed 's/SIZE/Size/' | sed 's/USE%/Use%/' | sed 's/FSTYPE/FSType/' | sed 's/TARGET/Directory/' \
    | sed 's/^/  /'
