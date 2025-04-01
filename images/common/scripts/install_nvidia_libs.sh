#!/bin/bash

set -x # Print actual command before executing it
set -e # Exit immediately if any command returns a non-zero error code

usage() { echo "usage: ${0} -j <path_to_jail_dir> -i <path_to_nvidia_container_cli_info> [-h]" >&2; exit 1; }

while getopts i:j:u:wh flag
do
    case "${flag}" in
        i) infopath=${OPTARG};;
        j) jaildir=${OPTARG};;
        h) usage;;
        *) usage;;
    esac
done

if [ -z "$jaildir" ] || [ -z "$infopath" ]; then
    usage
fi

if ! cmp -s <(nvidia-container-cli info --csv | head -2) "${infopath}" > /dev/null 2>&1; then
    echo "NVIDIA driver information has changed, updating configuration in the jail..."
    nvidia-container-cli \
        --user \
        --debug=/dev/stderr \
        --no-pivot \
        configure \
        --no-cgroups \
        --ldconfig="@$(command -v ldconfig.real || command -v ldconfig)" \
        --device=all \
        --utility \
        --compute \
        "${jaildir}"
    nvidia-container-cli info --csv | head -2 > "${infopath}"
else
    echo "No changes in NVIDIA driver information, skipping configuration"
fi
