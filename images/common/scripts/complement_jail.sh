#!/bin/bash

# Complement jaildir by bind-mounting virtual filesystems, users, and NVIDIA binaries from the host filesystem

set -x # Print actual command before executing it
set -euo pipefail

log() { { set +x; } 2>/dev/null; echo "[$(date -u +%H:%M:%S)] $*"; set -x; }

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

# Default optional variables so `set -u` doesn't abort: `worker` is only set via -w (unset on
# login nodes), and the GPU env vars may be absent depending on the cluster/nodeset.
worker="${worker:-}"
NODESET_GPU_ENABLED="${NODESET_GPU_ENABLED:-}"
SLURM_CLUSTER_WITH_GPU="${SLURM_CLUSTER_WITH_GPU:-}"
SOPERATOR_DOCKER_ENABLED="${SOPERATOR_DOCKER_ENABLED:-true}"

ALT_ARCH="$(uname -m)"
log "🔧 Using ALT_ARCH = ${ALT_ARCH}"

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
# The probe-based ldconfig block passes "skipped" when the probe reports the cache as up to date —
# an anomaly then means the probe was fooled by a broken view of the jail.
# Must be called with CWD = ${jaildir}.
verify_nvml_soname() {
    local how=$1
    local so1="usr/lib/${ALT_ARCH}-linux-gnu/libnvidia-ml.so.1"
    if [ "${how}" = "lock-busy" ] && [ ! -e "${so1}" ]; then
        # The lock holder is still running ldconfig; instead of polling with a guessed delay,
        # wait for the lock to be released — that happens exactly when the holder's ldconfig exits
        flock --wait 300 etc/complement_jail_ldconfig.lock -c true || echo "[nvml] timed out waiting for the ldconfig lock holder"
    fi
    if [ ! -e "${so1}" ]; then
        # Grace re-checks before declaring the anomaly: if the symlink shows up here, it
        # was created and only this view lagged
        local waited=0
        for delay in 1 5; do
            sleep "${delay}"
            waited=$((waited + delay))
            if [ -e "${so1}" ]; then
                echo "[nvml] libnvidia-ml.so.1 appeared after ${waited}s extra wait (view lag)"
                break
            fi
        done
    fi
    if [ -e "${so1}" ]; then
        echo "[nvml] libnvidia-ml.so.1 present after ldconfig phase (${how})"
    else
        echo "[nvml] ANOMALY: libnvidia-ml.so.1 absent after ldconfig phase (${how}) (SCHED-1041)"
    fi
    dump_nvml_state "after ldconfig phase (${how})"
}

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

        echo "[nvml] Jail mount: $(findmnt --target . --output SOURCE,FSTYPE,OPTIONS --noheadings 2>&1)"

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

        if [ "$SOPERATOR_DOCKER_ENABLED" = "true" ]; then
            log "[docker] Bind-mount dockerd stuff from container to the jail"
            mkdir -p etc/docker
            touch etc/docker/daemon.json
            mount --bind "/etc/docker/daemon.json" "etc/docker/daemon.json"
        else
            log "[docker] Docker is disabled, masking docker binaries in the jail"
            docker_disabled_stub=/opt/soperator-docker-disabled.sh
            cat > "$docker_disabled_stub" << 'EOF'
#!/bin/sh
echo "docker is not available on this cluster: it was deployed without image-storage disks required for storing Docker data" >&2
exit 1
EOF
            chmod 755 "$docker_disabled_stub"
            for docker_bin in usr/bin/docker usr/bin/dockerd; do
                if [ -f "$docker_bin" ]; then
                    mount --bind "$docker_disabled_stub" "$docker_bin"
                fi
            done
        fi

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

        # Gate the SCHED-1041 diagnostics on the cluster-level flag, not NODESET_GPU_ENABLED:
        # in a GPU cluster a CPU-nodeset worker can win the ldconfig flock too, and its run
        # decides whether the shared soname symlinks get created — it must not stay silent
        if [ "$SLURM_CLUSTER_WITH_GPU" = "true" ]; then
            dump_nvml_state "before ldconfig"
        fi

        log "[ldcache] Probing whether the linker cache is up to date"
        chroot "${jaildir}" /usr/sbin/ldconfig -p 2>/dev/null | grep '=>' | sort > "$current_cache_dump"
        chroot "${jaildir}" /usr/sbin/ldconfig -X -i -C "/${candidate_cache}"
        chroot "${jaildir}" /usr/sbin/ldconfig -C "/${candidate_cache}" -p 2>/dev/null | grep '=>' | sort > "$candidate_cache_dump"
        rm -f "${jaildir}/${candidate_cache}"

        ldconfig_how="skipped"
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
                ldconfig_how="ran"
            elif [ "$ldconfig_rc" -eq 75 ]; then
                log "[ldcache] ldconfig skipped: another worker holds the lock and is rebuilding the cache"
                ldconfig_how="lock-busy"
            else
                log "[ldcache] ldconfig FAILED with exit code ${ldconfig_rc}"
                if [ "$SLURM_CLUSTER_WITH_GPU" = "true" ]; then
                    verify_nvml_soname "failed"
                fi
                exit "$ldconfig_rc"
            fi
        fi
        rm -f "$current_cache_dump" "$candidate_cache_dump"
        if [ "$SLURM_CLUSTER_WITH_GPU" = "true" ]; then
            verify_nvml_soname "$ldconfig_how"
        fi
    fi
popd
