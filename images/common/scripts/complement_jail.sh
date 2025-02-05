#!/bin/bash

# Complement jaildir by bind-mounting virtual filesystems, users, and NVIDIA binaries from the host filesystem

set -x # Print actual command before executing it
set -e # Exit immediately if any command returns a non-zero error code

usage() { echo "usage: ${0} -j <path_to_jail_dir> -u <path_to_upper_jail_dir> [-w] [-h]" >&2; exit 1; }

while getopts j:u:wh flag
do
    case "${flag}" in
        j) jaildir=${OPTARG};;
        u) upperdir=${OPTARG};;
        w) worker=1;;
        h) usage;;
        *) usage;;
    esac
done

if [ -z "$jaildir" ] || [ -z "$upperdir" ]; then
    usage
fi

pushd "${jaildir}"
    echo "Bind-mount virtual filesystems"
    mount -t proc /proc proc/
    mount -t sysfs /sys sys/
    mount --rbind /dev dev/
    mount --rbind /run run/

    echo "Bind-mount cgroup filesystem"
    mount --rbind /sys/fs/cgroup sys/fs/cgroup

    echo "Remount /tmp"
    mount -t tmpfs tmpfs tmp/

    echo "Bind-mount /var/log because it should be node-local"
    mount --bind /var/log var/log

    echo "Bind-mount DNS configuration"
    mount --bind /etc/resolv.conf etc/resolv.conf

    echo "Bind-mount /etc/hosts"
    mount --bind /etc/hosts etc/hosts

    echo "Bind-mount jail submounts from upper ${upperdir} into the actual ${jaildir}"
    submounts=$( \
        findmnt --output TARGET --submounts --target / --pairs | \
        grep "^TARGET=\"${upperdir}/" | \
        sed -e "s|^TARGET=\"${upperdir}/||" -e "s|\"$||" \
    )
    while IFS= read -r path; do
        if [ -n "$path" ]; then
            echo "Bind-mount jail submount ${path}"
            if [ -d "${upperdir}/${path}" ]; then
                mkdir -p "${path}" # TODO: Support setting configurable permissions for jail submounts, this should be implemented in K8S VolumeMount
                mount --bind "${upperdir}/${path}" "${path}"
            elif [ -f "${upperdir}/${path}" ]; then
                # make sure mount point exists
                mkdir -p "$(dirname "${path}")/" && touch "${path}"
                mount --bind "${upperdir}/${path}" "${path}"
            fi
        fi
    done <<< "$submounts"

    if [ -n "$worker" ] && [ "$SLURM_CLUSTER_TYPE" = "gpu" ]; then
        echo "Run nvidia-container-cli to propagate NVIDIA drivers, CUDA, NVML and other GPU-related stuff to the jail"
        flock etc/complement_jail_nvidia_container_cli.lock -c "
            nvidia-container-cli \
                --user \
                --debug=/dev/stderr \
                --no-pivot \
                configure \
                --no-cgroups \
                --ldconfig=\"@$(command -v ldconfig.real || command -v ldconfig)\" \
                --device=all \
                --utility \
                --compute \
                \"${jaildir}\"
        "
        touch "etc/gpu_libs_installed.flag"
    fi

    echo "Bind-mount slurm chroot plugin from container to the jail"
    touch usr/lib/x86_64-linux-gnu/slurm/chroot.so
    mount --bind /usr/lib/x86_64-linux-gnu/slurm/chroot.so usr/lib/x86_64-linux-gnu/slurm/chroot.so

    echo "Bind-mount /etc/enroot, /usr/share/enroot and /usr/lib/enroot"
    mkdir -p etc/enroot usr/share/enroot usr/lib/enroot
    mount --bind /etc/enroot etc/enroot
    mount --bind /usr/share/enroot usr/share/enroot
    mount --bind /usr/lib/enroot usr/lib/enroot

    echo "Bind-mount enroot binaries"
    for file in /usr/bin/enroot*; do
        filename=$(basename "$file")
        touch "usr/bin/$filename" && mount --bind "$file" "usr/bin/$filename"
    done

    echo "Bind-mount enroot data directory because it should be node-local"
    mount --bind /usr/share/enroot/enroot-data usr/share/enroot/enroot-data

    if ! getcap usr/bin/enroot-mksquashovlfs | grep -q 'cap_sys_admin+pe'; then
        echo "Set capabilities for enroot-mksquashovlfs to run containers without privileges"
        flock etc/complement_jail_setcap_enroot_mksquashovlfs.lock -c "setcap cap_sys_admin+pe usr/bin/enroot-mksquashovlfs"
    fi
    if ! getcap usr/bin/enroot-aufs2ovlfs | grep -q 'cap_sys_admin,cap_mknod+pe'; then
        echo "Set capabilities for enroot-aufs2ovlfs to run containers without privileges"
        flock etc/complement_jail_setcap_enroot_aufs2ovlfs.lock -c "setcap cap_sys_admin,cap_mknod+pe usr/bin/enroot-aufs2ovlfs"
    fi

    echo "Shared folder for pyxis output /var/cache/enroot-container-images"
    mkdir -m 1777 -p var/cache/enroot-container-images

    echo "Bind-mount pyxis plugin from container to the jail"
    touch usr/lib/x86_64-linux-gnu/slurm/spank_pyxis.so
    mount --bind /usr/lib/x86_64-linux-gnu/slurm/spank_pyxis.so usr/lib/x86_64-linux-gnu/slurm/spank_pyxis.so

    echo "Bind-mount slurm configs"
    for file in /mnt/slurm-configs/*; do
        filename=$(basename "$file")
        touch "etc/slurm/$filename" && mount --bind "$file" "etc/slurm/$filename"
    done

    if [ -n "$worker" ]; then
        echo "Bind-mount slurmd spool directory from the host because it should be propagated to the jail"
        mount --bind /var/spool/slurmd var/spool/slurmd/
    fi

    # For login node with cluster type GPU
    if [ -z "$worker" ] && [ "$SLURM_CLUSTER_TYPE" = "gpu" ]; then
        while [ ! -f "etc/gpu_libs_installed.flag" ]; do
            echo "Waiting for GPU libs to be propagated to the jail from a worker node"
            sleep 10
        done
        echo "Bind-mount all GPU-specific empty lib files into the host's libdummy"
        find "${jaildir}/lib/x86_64-linux-gnu" -maxdepth 1 -type f ! -type l -empty -print0 | while IFS= read -r -d '' file; do
            mount --bind "/lib/x86_64-linux-gnu/libdummy.so" "$file"
        done
    fi

    # For $worker node only
    if [ -n "$worker" ]; then
        echo "Update linker cache inside the jail"
        flock etc/complement_jail_ldconfig.lock -c "chroot \"${jaildir}\" /usr/sbin/ldconfig"
    fi
popd
