#!/bin/bash

# Complement jaildir by bind-mounting virtual filesystems, users, and NVIDIA binaries from the host filesystem

set -x # Print actual command before executing it
set -euo pipefail

log() { { set +x; } 2>/dev/null; echo "[$(date -u +%H:%M:%S)] $*"; set -x; }

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

# Default optional variables so `set -u` doesn't abort: `worker` is only set via -w (unset on
# login nodes), and the GPU env vars may be absent depending on the cluster/nodeset.
worker="${worker:-}"
NODESET_GPU_ENABLED="${NODESET_GPU_ENABLED:-}"
SLURM_CLUSTER_WITH_GPU="${SLURM_CLUSTER_WITH_GPU:-}"

ALT_ARCH="$(uname -m)"
log "🔧 Using ALT_ARCH = ${ALT_ARCH}"

SLURM_LIB_PATH="usr/lib/${ALT_ARCH}-linux-gnu/slurm"

pushd "${jaildir}"
    log "[virtual-fs] Bind-mount virtual filesystems"
    mount -t proc /proc proc/
    mount -t sysfs /sys sys/
    mount --rbind /dev dev/
    mount --rbind /run run/

    log "[virtual-fs] Bind-mount cgroup filesystem"
    mount --rbind /sys/fs/cgroup sys/fs/cgroup

    log "[virtual-fs] Bind-mount /tmp"
    mount --bind /tmp tmp/

    log "[virtual-fs] Bind-mount /var/log because it should be node-local"
    mount --bind /var/log var/log

    log "[dns] Bind-mount DNS configuration"
    mount --bind /etc/resolv.conf etc/resolv.conf

    log "[dns] Bind-mount /etc/hosts"
    mount --bind /etc/hosts etc/hosts

    log "[sssd] Bind mount sssd.conf if exists"
    if [[ -f /etc/sssd/sssd.conf ]]; then
      mount --bind /etc/sssd etc/sssd
    fi

    log "[sssd] Bind-mount SSSD sockets if they exist"
    if [[ -d /var/lib/sss/pipes ]]; then
      mkdir -p var/lib/sss/pipes
      mount --bind /var/lib/sss/pipes var/lib/sss/pipes
    fi

    log "[submounts] Bind-mount jail submounts from upper ${upperdir} into the actual ${jaildir}"
    submounts=$( \
        findmnt --output TARGET --submounts --target / --pairs | \
        grep "^TARGET=\"${upperdir}/" | \
        sed -e "s|^TARGET=\"${upperdir}/||" -e "s|\"$||" \
    )
    while IFS= read -r path; do
        if [ -n "$path" ]; then
            log "[submounts] Bind-mount jail submount ${path}"
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
        log "[gpu] Run nvidia-container-cli to propagate NVIDIA drivers, CUDA, NVML and other GPU-related stuff to the jail"

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

        nvidia-container-cli \
            --user \
            --debug=/dev/stderr \
            --no-pivot \
            configure \
            --no-cgroups \
            --cuda-compat-mode=disabled \
            --ldconfig=$FAKE_LDCONFIG \
            --device=all \
            "${cap_args[@]}" \
            "${jaildir}"

        log "[gpu] Checking NVIDIA lib state after nvidia-container-cli:"
        echo "  /lib symlink: $(readlink lib 2>/dev/null || echo 'NOT a symlink')"
        echo "  libnvidia-ml.so.1 exists: $(test -e usr/lib/${ALT_ARCH}-linux-gnu/libnvidia-ml.so.1 && echo 'yes' || echo 'NO')"
        echo "  libnvidia-ml.so.1 target: $(readlink usr/lib/${ALT_ARCH}-linux-gnu/libnvidia-ml.so.1 2>/dev/null || echo 'MISSING')"
        echo "  libnvidia-ml.so versioned file size: $(stat -c%s usr/lib/${ALT_ARCH}-linux-gnu/libnvidia-ml.so.*.* 2>/dev/null || echo 'NOT FOUND')"

        touch "etc/gpu_libs_installed.flag"
    fi

    log "[slurm] Bind-mount slurm client"
    /opt/bin/slurm/bind_slurm_common.sh -j "${jaildir}"

    log "[slurm] Bind-mount slurm chroot plugin from container to the jail"
    mkdir -p usr/lib/slurm
    touch usr/lib/slurm/chroot.so
    mount --bind "/${SLURM_LIB_PATH}/chroot.so" "usr/lib/slurm/chroot.so"

    log "[slurm] Bind-mount NCCL debug SPANK plugin from container to the jail"
    touch usr/lib/slurm/spanknccldebug.so
    mount --bind "/${SLURM_LIB_PATH}/spanknccldebug.so" "usr/lib/slurm/spanknccldebug.so"

    log "[slurm] Bind-mount NCCL Inspector PreConf SPANK plugin from container to the jail"
    touch usr/lib/slurm/spank_nccl_inspector_preconf.so
    mount --bind "/${SLURM_LIB_PATH}/spank_nccl_inspector_preconf.so" "usr/lib/slurm/spank_nccl_inspector_preconf.so"

    log "[enroot] Bind-mount /etc/enroot, /usr/share/enroot and /usr/lib/enroot"
    mkdir -p etc/enroot usr/share/enroot usr/lib/enroot
    mkdir -p /etc/enroot/mounts.d
    printf '%s\n' '/opt/slurm_scripts/task_prolog.sh /opt/slurm_scripts/task_prolog.sh none x-create=file,bind,ro,nosuid,nodev,private,nofail 0 0' | sudo tee /etc/enroot/mounts.d/task_prolog.fstab
    mount --rbind /etc/enroot etc/enroot
    mount --bind /usr/share/enroot usr/share/enroot
    mount --bind /usr/lib/enroot usr/lib/enroot

    log "[enroot] Bind-mount enroot binaries"
    for file in /usr/bin/enroot*; do
        filename=$(basename "$file")
        touch "usr/bin/$filename" && mount --bind "$file" "usr/bin/$filename"
    done

    if ! getcap usr/bin/enroot-mksquashovlfs | grep -q 'cap_sys_admin+pe'; then
        log "[enroot] Set capabilities for enroot-mksquashovlfs to run containers without privileges"
        flock etc/complement_jail_setcap_enroot_mksquashovlfs.lock -c "setcap cap_sys_admin+pe usr/bin/enroot-mksquashovlfs"
    fi
    if ! getcap usr/bin/enroot-aufs2ovlfs | grep -q 'cap_sys_admin,cap_mknod+pe'; then
        log "[enroot] Set capabilities for enroot-aufs2ovlfs to run containers without privileges"
        flock etc/complement_jail_setcap_enroot_aufs2ovlfs.lock -c "setcap cap_sys_admin,cap_mknod+pe usr/bin/enroot-aufs2ovlfs"
    fi

    log "[enroot] Create shared directories for caching Pyxis sqshfs files and Enroot OCI data"
    install -d -m 1777 var/cache/enroot-container-images var/cache/enroot

    log "[pyxis] Bind-mount pyxis plugin from container to the jail"
    touch "usr/lib/slurm/spank_pyxis.so"
    mount --bind "/${SLURM_LIB_PATH}/spank_pyxis.so" "usr/lib/slurm/spank_pyxis.so"

    log "[outputs] Creating Soperator output directory"
    mkdir -m 777 -p opt/soperator-outputs
    # The NCCL debug SPANK plugin writes per-job logs here; ensure it exists and is world-writable
    # on every startup so all clusters have it (previously done by the ensure-dir-snccld-logs active check).
    mkdir -m 777 -p opt/soperator-outputs/nccl_logs

    # For login nodes in GPU clusters and CPU workers in GPU clusters
    if { [ -z "$worker" ] && [ "$SLURM_CLUSTER_WITH_GPU" = "true" ]; } || { [ -n "$worker" ] && [ "$SLURM_CLUSTER_WITH_GPU" = "true" ] && [ "$NODESET_GPU_ENABLED" != "true" ]; }; then
        while [ ! -f "etc/gpu_libs_installed.flag" ]; do
            log "[gpu-wait] Waiting for GPU libs to be propagated to the jail from a worker node"
            sleep 10
        done
        log "[gpu-wait] Bind-mount all GPU-specific empty lib files into the host's libdummy"
        find "${jaildir}/lib/${ALT_ARCH}-linux-gnu" -maxdepth 1 -type f ! -type l -empty -print0 | while IFS= read -r -d '' file; do
            mount --bind "/lib/${ALT_ARCH}-linux-gnu/libdummy.so" "$file"
        done
    fi

    # For worker node only
    if [ -n "$worker" ]; then
        log "[slurmd] Bind-mount slurmd spool directory from the host because it should be propagated to the jail"
        mount --bind /var/spool/slurmd var/spool/slurmd/

        # slurmd package tree https://gist.github.com/asteny/9eb5089a10a793834d12a5b2449cc2b9
        log "[slurmd] Bind-mount slurmd binaries from container to the jail"
        touch usr/sbin/slurmd usr/sbin/slurmstepd
        mount --bind /usr/sbin/slurmd usr/sbin/slurmd
        mount --bind /usr/sbin/slurmstepd usr/sbin/slurmstepd

        log "[docker] Bind-mount dockerd stuff from container to the jail"
        mkdir -p etc/docker
        touch etc/docker/daemon.json
        mount --bind "/etc/docker/daemon.json" "etc/docker/daemon.json"

        # ld.so.cache lives in the shared jail root, so running ldconfig rewrites the cache
        # that every running job on every node depends on, briefly leaving it empty. To avoid
        # this on each worker pod start (e.g. node auto-replacement), rebuild the shared cache
        # only when its contents would actually change.
        #
        # The check builds a throwaway candidate cache from the libraries currently visible in
        # the jail and compares it with the cache already in place; the real ldconfig runs only
        # if they differ. This detects any library change (driver upgrade, OpenMPI, custom
        # libs) without a version heuristic or a shared marker file.
        #
        # /tmp inside the jail is node-local (bind-mounted from the host), so the candidate
        # cache plus -X (don't update symlinks) and -i (don't touch the shared aux-cache)
        # keep the shared jail untouched while we probe. The first line of `ldconfig -p`
        # is a header that embeds the cache path, so compare only the entry lines (=>).
        current_cache_dump=$(mktemp)
        candidate_cache_dump=$(mktemp)
        candidate_cache="tmp/ld.so.cache.candidate.$$"

        log "[ldcache] Probing whether the linker cache is up to date"
        chroot "${jaildir}" /usr/sbin/ldconfig -p 2>/dev/null | grep '=>' | sort > "$current_cache_dump"
        chroot "${jaildir}" /usr/sbin/ldconfig -X -i -C "/${candidate_cache}"
        chroot "${jaildir}" /usr/sbin/ldconfig -C "/${candidate_cache}" -p 2>/dev/null | grep '=>' | sort > "$candidate_cache_dump"
        rm -f "${jaildir}/${candidate_cache}"

        if cmp -s "$current_cache_dump" "$candidate_cache_dump"; then
            log "[ldcache] Linker cache is already up to date, skipping ldconfig"
        else
            log "[ldcache] Linker cache is stale, updating it inside the jail"
            ldconfig_rc=0
            # --conflict-exit-code distinguishes "another worker holds the lock" (a benign skip,
            # that worker rebuilds the shared cache) from a real ldconfig failure, which must
            # fail the entry point so the container restarts instead of leaving a broken cache.
            flock --nonblock --conflict-exit-code 75 etc/complement_jail_ldconfig.lock \
                -c "chroot \"${jaildir}\" /usr/sbin/ldconfig" || ldconfig_rc=$?
            if [ "$ldconfig_rc" -eq 0 ]; then
                log "[ldcache] ldconfig completed successfully (got flock)"
            elif [ "$ldconfig_rc" -eq 75 ]; then
                log "[ldcache] ldconfig skipped: another worker holds the lock and is rebuilding the cache"
            else
                log "[ldcache] ldconfig FAILED with exit code ${ldconfig_rc}"
                exit "$ldconfig_rc"
            fi
        fi
        rm -f "$current_cache_dump" "$candidate_cache_dump"
    fi
popd
