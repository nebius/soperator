#!/bin/bash

# Complement jaildir by bind-mounting virtual filesystems, users, and NVIDIA binaries from the host filesystem

set -e # Exit immediately if any command returns a non-zero error code

usage() { echo "usage: ${0} -j <path_to_jail_dir> [-w] [-h]" >&2; exit 1; }

while getopts j:wh flag
do
    case "${flag}" in
        j) jaildir=${OPTARG};;
        w) worker=1;;
        h) usage;;
        *) usage;;
    esac
done

if [ -z "$jaildir" ]; then
    usage
fi

pushd "${jaildir}"
    echo "Bind-mount virtual filesystems"
    mount -t proc /proc proc/
    mount -t sysfs /sys sys/
    mount --rbind /dev dev/
    mount --rbind /run run/

    echo "Remount /tmp"
    # TODO: Maybe we should do this after nvidia-container-cli?
    mount -t tmpfs tmpfs tmp/

    echo "Bind-mount /var/log because it should be node-local"
    mount --bind /var/log var/log

    echo "Bind-mount DNS configuration"
    mount --bind /etc/resolv.conf etc/resolv.conf

    echo "Bind-mount /etc/hosts"
    mount --bind /etc/hosts etc/hosts

    if [ -n "$worker" ]; then
        echo "Run nvidia-container-cli to propagate NVIDA drivers, CUDA, NVML and other GPU-related stuff to the jail"
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
        touch "etc/gpu_libs_installed.flag"
    fi

    if [ -n "$worker" ]; then
        echo "Bind-mount enroot data directory because if should be node-local"
        mount --bind /usr/share/enroot/enroot-data usr/share/enroot/enroot-data
    fi

    echo "Bind-mount slurm configs"
    for file in /mnt/slurm-configs/*; do
        filename=$(basename "$file")
        touch "etc/slurm/$filename" && mount --bind "$file" "etc/slurm/$filename"
    done

    if [ -n "$worker" ]; then
        echo "Bind-mount slurmd spool directory from the host because it should be propagated to the jail"
        mount --bind /var/spool/slurmd var/spool/slurmd/
    fi

    if [ -z "$worker" ]; then
        while [ ! -f "etc/gpu_libs_installed.flag" ]; do
            echo "Waiting for GPU libs to be propagated to the jail from a worker node"
            sleep 10
        done
        echo "Bind-mount all GPU-specific empty lib files into the host's libdummy"
        find "${jaildir}/lib/x86_64-linux-gnu" -maxdepth 1 -type f ! -type l -empty -print0 | while IFS= read -r -d '' file; do
            mount --bind "/lib/x86_64-linux-gnu/libdummy.so" "$file"
        done
    fi

    if [ -n "$worker" ]; then
        echo "Update linker cache inside the jail"
        chroot "${jaildir}" /usr/sbin/ldconfig
    fi
popd
