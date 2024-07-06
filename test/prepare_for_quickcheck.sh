#!/bin/bash

usage() { echo "usage: ${0} -u <ssh_user> -k <path_to_ssh_key> -a <address> [-h]" >&2; exit 1; }

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

scp -i "${key}" -r ./demo "${user}"@"${address}":/
