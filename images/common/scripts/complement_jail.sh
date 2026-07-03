#!/bin/bash

# Complement jaildir by bind-mounting virtual filesystems, users, and NVIDIA binaries from the host filesystem

set -x # Print actual command before executing it
set -e # Exit immediately if any command returns a non-zero error code

# Timestamp traced commands to correlate concurrent workers complementing the shared jail
PS4='+ [$(date "+%H:%M:%S.%3N")] '

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

ALT_ARCH="$(uname -m)"
echo "🔧 Using ALT_ARCH = ${ALT_ARCH}"

SLURM_LIB_PATH="usr/lib/${ALT_ARCH}-linux-gnu/slurm"

# SCHED-1041 diagnostics.
# The NVIDIA soname symlinks (e.g. libnvidia-ml.so.1) are created only by the "chroot ldconfig" run below,
# and sometimes the worker that wins the ldconfig flock has a stale view of the jail lib dir right after
# its own nvidia-container-cli run, so its ldconfig creates no sonames and GPU containers fail on every node of the cluster.
# How to read the output:
# - path/chroot view has no libnvidia-ml at all -> stale dentry/readdir view of the shared
#   jail, or the CLI mounted into another namespace (check the mountinfo counts below)
# - 0-size versioned libs -> shared-FS placeholders visible, bind mounts detached
# - bind mounts present in mountinfo while invisible by path -> stale dentries (virtiofs)
# - bind mounts absent from mountinfo -> the CLI's mounts landed in another namespace
# - "self" and "via jail proc" namespaces differ -> the CLI targeted a wrong namespace:
#   it derives the target from ${jaildir}/proc/<its parent pid>/ns/mnt
# Must be called with CWD = ${jaildir}. All output is prefixed with [nvml] for grepping.
# shellcheck disable=SC2012 # ls is deliberate: the probe must exercise readdir on the live directory
dump_nvml_state() {
    echo "[nvml] === NVML jail state: ${1} ==="
    echo "[nvml] --- path view:"
    ls -lai "usr/lib/${ALT_ARCH}-linux-gnu/" 2>&1 | awk '/libnvidia-ml|libcuda\.so/ {print "[nvml]   " $0; n++} END {if (!n) print "[nvml]   no libnvidia-ml/libcuda entries visible by path"}'
    echo "[nvml] --- chroot view (what ldconfig sees):"
    chroot "${jaildir}" ls -lai "/usr/lib/${ALT_ARCH}-linux-gnu/" 2>&1 | awk '/libnvidia-ml|libcuda\.so/ {print "[nvml]   " $0; n++} END {if (!n) print "[nvml]   no libnvidia-ml/libcuda entries visible via chroot"}'
    echo "[nvml] --- nvidia lib bind mounts in this namespace: $(grep -cF "${jaildir}/usr/lib/${ALT_ARCH}-linux-gnu/libnvidia" /proc/self/mountinfo) (libnvidia-ml: $(grep -cF "${jaildir}/usr/lib/${ALT_ARCH}-linux-gnu/libnvidia-ml" /proc/self/mountinfo))"
    echo "[nvml] --- mount namespace self: $(readlink "/proc/$$/ns/mnt") via jail proc: $(readlink "${jaildir}/proc/$$/ns/mnt" 2>&1)"
    echo "[nvml] === end NVML jail state: ${1} ==="
}

# Verify the NVIDIA soname symlink exists after the ldconfig phase and log the SCHED-1041 anomaly otherwise.
# ${1} says what happened on this worker:
# - "ran" (ldconfig executed here),
# - "lock-busy" (another worker holds the ldconfig lock),
# - "skipped" (this worker decided not to run it), or
# - "failed".
# Kept generic so main's probe-based ldconfig block (PR #2621) can call it with the same statuses ("skipped" when
# the probe reports the cache as up to date — an anomaly then means the probe was fooled by a broken view of the jail).
# Must be called with CWD = ${jaildir}.
verify_nvml_soname() {
    local how=$1
    local so1="usr/lib/${ALT_ARCH}-linux-gnu/libnvidia-ml.so.1"
    if [ "${how}" = "lock-busy" ] && [ ! -e "${so1}" ]; then
        # The lock winner may still be running ldconfig; wait to see whether its run ever
        # produces the soname symlink on the shared jail. If it never does, the winner ran
        # against a broken view
        for _ in 1 2 3 4 5 6; do
            sleep 5
            if [ -e "${so1}" ]; then
                break
            fi
        done
    fi
    if [ -e "${so1}" ]; then
        echo "[nvml] libnvidia-ml.so.1 present after ldconfig phase (${how})"
    else
        echo "[nvml] ANOMALY: libnvidia-ml.so.1 absent after ldconfig phase (${how}) (SCHED-1041)"
        # If the symlink appears after a delay, it was created and only this view lagged;
        # if it stays absent, whoever ran ldconfig saw no NVIDIA libs, or nobody ran it at all
        local waited=0
        for delay in 1 5; do
            sleep "${delay}"
            waited=$((waited + delay))
            if [ -e "${so1}" ]; then
                echo "[nvml] libnvidia-ml.so.1 appeared after ${waited}s"
                break
            fi
            echo "[nvml] libnvidia-ml.so.1 still absent after ${waited}s"
        done
    fi
    dump_nvml_state "after ldconfig phase (${how})"
}

pushd "${jaildir}"
    echo "Bind-mount virtual filesystems"
    mount -t proc /proc proc/
    mount -t sysfs /sys sys/
    mount --rbind /dev dev/
    mount --rbind /run run/

    echo "Bind-mount cgroup filesystem"
    mount --rbind /sys/fs/cgroup sys/fs/cgroup

    echo "Bind-mount /tmp"
    mount --bind /tmp tmp/

    echo "Bind-mount /var/log because it should be node-local"
    mount --bind /var/log var/log

    echo "Bind-mount DNS configuration"
    mount --bind /etc/resolv.conf etc/resolv.conf

    echo "Bind-mount /etc/hosts"
    mount --bind /etc/hosts etc/hosts

    echo "Bind mount sssd.conf if exists"
    if [[ -f /etc/sssd/sssd.conf ]]; then
      mount --bind /etc/sssd etc/sssd
    fi

    echo "Bind-mount SSSD sockets if they exist"
    if [[ -d /var/lib/sss/pipes ]]; then
      mkdir -p var/lib/sss/pipes
      mount --bind /var/lib/sss/pipes var/lib/sss/pipes
    fi

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
                mount --rbind "${upperdir}/${path}" "${path}"
            elif [ -f "${upperdir}/${path}" ]; then
                # make sure mount point exists
                mkdir -p "$(dirname "${path}")/" && touch "${path}"
                mount --bind "${upperdir}/${path}" "${path}"
            fi
        fi
    done <<< "$submounts"

    if [ -n "$worker" ] && [ "$NODESET_GPU_ENABLED" = "true" ]; then
        echo "Run nvidia-container-cli to propagate NVIDIA drivers, CUDA, NVML and other GPU-related stuff to the jail"

        # Disable ldconfig to prevent race between the workers.
        # ldconfig is run further in this script under flock.
        readonly FAKE_LDCONFIG=/usr/bin/true

        # Propagate the same capabilities as in NVIDIA_DRIVER_CAPABILITIES
        cap_args=()
        if [ -z "${NVIDIA_DRIVER_CAPABILITIES-}" ]; then
            NVIDIA_DRIVER_CAPABILITIES="compute,utility"
        fi
        for cap in ${NVIDIA_DRIVER_CAPABILITIES//,/ }; do
            case "${cap}" in
            all)
                cap_args+=("--compute" "--compat32" "--display" "--graphics" "--utility" "--video")
                break
                ;;
            compute|compat32|display|graphics|utility|video)
                cap_args+=("--${cap}") ;;
            *)
                echo "Unknown NVIDIA driver capability: ${cap}"
                exit 1
                ;;
            esac
        done

        echo "[nvml] Jail mount: $(findmnt --target . --output SOURCE,FSTYPE,OPTIONS --noheadings 2>&1)"

        nvidia-container-cli \
            --user \
            --debug=/dev/stderr \
            --no-pivot \
            configure \
            --no-cgroups \
            --ldconfig=$FAKE_LDCONFIG \
            --device=all \
            "${cap_args[@]}" \
            "${jaildir}"

        echo "[nvml] /lib symlink: $(readlink lib 2>/dev/null || echo 'NOT a symlink')"
        dump_nvml_state "after nvidia-container-cli"

        # Local-coherence probe: a file created through this view must be immediately visible;
        # if it is not, the whole directory view is incoherent, not just the freshly created bind mounts
        probe="usr/lib/${ALT_ARCH}-linux-gnu/.soperator-jail-probe-$(hostname)-$$"
        touch "${probe}" || echo "[nvml] probe touch FAILED"
        if [ -e "${probe}" ]; then
            echo "[nvml] probe file visible right after touch: yes"
        else
            echo "[nvml] probe file visible right after touch: NO (local writes invisible through this view)"
        fi
        rm -f "${probe}" || true

        # If the versioned lib only appears after a delay, the staleness is transient and a retry-based fix is viable;
        # if it never appears, the view stays broken
        if ! compgen -G "usr/lib/${ALT_ARCH}-linux-gnu/libnvidia-ml.so.*.*" > /dev/null; then
            waited=0
            for delay in 1 5; do
                sleep "${delay}"
                waited=$((waited + delay))
                if compgen -G "usr/lib/${ALT_ARCH}-linux-gnu/libnvidia-ml.so.*.*" > /dev/null; then
                    echo "[nvml] versioned libnvidia-ml appeared after ${waited}s"
                    break
                fi
                echo "[nvml] versioned libnvidia-ml still not visible after ${waited}s"
            done
            dump_nvml_state "after visibility wait"
        fi

        touch "etc/gpu_libs_installed.flag"
    fi

    echo "Bind-mount slurm client"
    /opt/bin/slurm/bind_slurm_common.sh -j "${jaildir}"

    echo "Bind-mount slurm chroot plugin from container to the jail"
    mkdir -p usr/lib/slurm
    touch usr/lib/slurm/chroot.so
    mount --bind "/${SLURM_LIB_PATH}/chroot.so" "usr/lib/slurm/chroot.so"

    echo "Bind-mount NCCL debug SPANK plugin from container to the jail"
    touch usr/lib/slurm/spanknccldebug.so
    mount --bind "/${SLURM_LIB_PATH}/spanknccldebug.so" "usr/lib/slurm/spanknccldebug.so"

    echo "Bind-mount /etc/enroot, /usr/share/enroot and /usr/lib/enroot"
    mkdir -p etc/enroot usr/share/enroot usr/lib/enroot
    mount --rbind /etc/enroot etc/enroot
    mount --bind /usr/share/enroot usr/share/enroot
    mount --bind /usr/lib/enroot usr/lib/enroot

    echo "Bind-mount enroot binaries"
    for file in /usr/bin/enroot*; do
        filename=$(basename "$file")
        touch "usr/bin/$filename" && mount --bind "$file" "usr/bin/$filename"
    done

    if ! getcap usr/bin/enroot-mksquashovlfs | grep -q 'cap_sys_admin+pe'; then
        echo "Set capabilities for enroot-mksquashovlfs to run containers without privileges"
        flock etc/complement_jail_setcap_enroot_mksquashovlfs.lock -c "setcap cap_sys_admin+pe usr/bin/enroot-mksquashovlfs"
    fi
    if ! getcap usr/bin/enroot-aufs2ovlfs | grep -q 'cap_sys_admin,cap_mknod+pe'; then
        echo "Set capabilities for enroot-aufs2ovlfs to run containers without privileges"
        flock etc/complement_jail_setcap_enroot_aufs2ovlfs.lock -c "setcap cap_sys_admin,cap_mknod+pe usr/bin/enroot-aufs2ovlfs"
    fi

    echo "Create shared directory for caching Pyxis sqshfs files"
    mkdir -m 1777 -p var/cache/enroot-container-images

    echo "Bind-mount pyxis plugin from container to the jail"
    touch "usr/lib/slurm/spank_pyxis.so"
    mount --bind "/${SLURM_LIB_PATH}/spank_pyxis.so" "usr/lib/slurm/spank_pyxis.so"

    echo 'Creating Soperator output directory'
    mkdir -m 777 -p opt/soperator-outputs

    # For login nodes in GPU clusters and CPU workers in GPU clusters
    if { [ -z "$worker" ] && [ "$SLURM_CLUSTER_WITH_GPU" = "true" ]; } || { [ -n "$worker" ] && [ "$SLURM_CLUSTER_WITH_GPU" = "true" ] && [ "$NODESET_GPU_ENABLED" != "true" ]; }; then
        while [ ! -f "etc/gpu_libs_installed.flag" ]; do
            echo "Waiting for GPU libs to be propagated to the jail from a worker node"
            sleep 10
        done
        echo "Bind-mount all GPU-specific empty lib files into the host's libdummy"
        find "${jaildir}/lib/${ALT_ARCH}-linux-gnu" -maxdepth 1 -type f ! -type l -empty -print0 | while IFS= read -r -d '' file; do
            mount --bind "/lib/${ALT_ARCH}-linux-gnu/libdummy.so" "$file"
        done
    fi

    # For worker node only
    if [ -n "$worker" ]; then
        echo "Bind-mount slurmd spool directory from the host because it should be propagated to the jail"
        mount --bind /var/spool/slurmd var/spool/slurmd/
        # slurmd package tree https://gist.github.com/asteny/9eb5089a10a793834d12a5b2449cc2b9
        echo "Bind-mount slurmd binaries from container to the jail"
        touch usr/sbin/slurmd usr/sbin/slurmstepd
        mount --bind /usr/sbin/slurmd usr/sbin/slurmd
        mount --bind /usr/sbin/slurmstepd usr/sbin/slurmstepd
        echo "Update linker cache inside the jail"
        if [ "$NODESET_GPU_ENABLED" = "true" ]; then
            dump_nvml_state "before ldconfig"
        fi
        ldconfig_rc=0
        flock --nonblock etc/complement_jail_ldconfig.lock -c "chroot \"${jaildir}\" /usr/sbin/ldconfig" || ldconfig_rc=$?
        if [ "$ldconfig_rc" -eq 0 ]; then
            echo "ldconfig completed successfully (got flock)"
        elif [ "$ldconfig_rc" -eq 1 ]; then
            echo "ldconfig SKIPPED (flock busy)"
        else
            echo "ldconfig FAILED with exit code ${ldconfig_rc}"
        fi
        if [ "$NODESET_GPU_ENABLED" = "true" ]; then
            case "$ldconfig_rc" in
                0) verify_nvml_soname "ran" ;;
                1) verify_nvml_soname "lock-busy" ;;
                *) verify_nvml_soname "failed" ;;
            esac
        fi
    fi
popd
