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

# NVML diagnostics dump, [nvml]-prefixed for grepping. Must be called with CWD = ${jaildir}.
# The failure mechanism these dumps watch for: when one worker's nvidia-container-cli fails, its
# cleanup unmounts and DELETES every 0-byte mount target it had mounted on the shared jail
# (nvc_mount.c unmount() -> file_remove()), unlinking the placeholders under the other workers' live bind mounts;
# their kernels then detach those mounts. precreate_gpu_placeholders below makes the placeholders non-empty
# and thus immune; these dumps are the telemetry proving that.
# shellcheck disable=SC2012 # ls is deliberate: the probe must exercise readdir on the live directory
dump_nvml_state() {
    echo "[nvml] === NVML jail state: ${1} ==="

    # No libnvidia-ml entries at all -> the placeholders were deleted (a neighbor's CLI failed and its cleanup ran);
    # 0-size or marker-size versioned libs -> this worker's bind mounts are gone
    echo "[nvml] --- path view:"
    ls -lai "usr/lib/${ALT_ARCH}-linux-gnu/" 2>&1 | awk '/libnvidia-ml|libcuda\.so/ {print "[nvml]   " $0; n++} END {if (!n) print "[nvml]   no libnvidia-ml/libcuda entries visible by path"}'

    # A difference from the path view means ldconfig resolves the jail through another view
    echo "[nvml] --- chroot view (what ldconfig sees):"
    chroot "${jaildir}" ls -lai "/usr/lib/${ALT_ARCH}-linux-gnu/" 2>&1 | awk '/libnvidia-ml|libcuda\.so/ {print "[nvml]   " $0; n++} END {if (!n) print "[nvml]   no libnvidia-ml/libcuda entries visible via chroot"}'

    # 0 mounts while the libs above look healthy -> the CLI's mounts landed in another namespace
    echo "[nvml] --- nvidia lib bind mounts in this namespace: $(grep -cF "${jaildir}/usr/lib/${ALT_ARCH}-linux-gnu/libnvidia" /proc/self/mountinfo) (libnvidia-ml: $(grep -cF "${jaildir}/usr/lib/${ALT_ARCH}-linux-gnu/libnvidia-ml" /proc/self/mountinfo))"

    # Differing values -> the CLI targeted a wrong namespace: it derives the target
    # from ${jaildir}/proc/<its parent pid>/ns/mnt
    echo "[nvml] --- mount namespace self: $(readlink "/proc/$$/ns/mnt") via jail proc: $(readlink "${jaildir}/proc/$$/ns/mnt" 2>&1)"
    echo "[nvml] === end NVML jail state: ${1} ==="
}

# Placeholders are non-empty marker files now (see precreate_gpu_placeholders), so `-s` no longer tells
# a real library from an unmounted placeholder — require real content instead.
# Follows symlinks; a missing file or a dangling link counts as no content.
lib_has_real_content() {
    [ "$(stat -Lc %s "$1" 2>/dev/null || echo 0)" -gt 1024 ]
}

# Make sure the NVIDIA soname symlink exists AND points at real content after the ldconfig phase:
# repair by running ldconfig here if not, and restart the container when the jail still has no
# working NVML. ${1} says what happened on this worker:
# - "ran" (ldconfig executed here),
# - "lock-busy" (another worker holds the ldconfig lock),
# - "skipped" (this worker decided not to run it), or
# - "failed".
# The probe-based ldconfig block passes "skipped" when the probe reports the cache as up to date —
# an anomaly then means the probe was fooled by a broken view of the jail.
# lib_has_real_content follows the symlink, so a dangling link or a placeholder target counts as broken.
# Must be called with CWD = ${jaildir}.
ensure_nvml_soname() {
    local how=$1
    local so1="usr/lib/${ALT_ARCH}-linux-gnu/libnvidia-ml.so.1"
    # A GPU worker whose capability set excludes `utility` gets no NVML mounted by design, so no
    # soname is expected (NVML consumers like the enroot hook are knowingly opted out with it).
    # CPU workers in GPU clusters keep the check: they see the soname over their libdummy mounts.
    if [ "$NODESET_GPU_ENABLED" = "true" ] && [ "$(grep -cF "${jaildir}/usr/lib/${ALT_ARCH}-linux-gnu/libnvidia-ml" /proc/self/mountinfo)" -lt 1 ]; then
        echo "[nvml] libnvidia-ml not mounted (capability set excludes utility), skipping the soname check"
        return 0
    fi
    if [ "${how}" = "lock-busy" ] && ! lib_has_real_content "${so1}"; then
        # The lock holder is still running ldconfig; instead of polling with a guessed delay,
        # wait for the lock to be released — that happens exactly when the holder's ldconfig exits
        flock --wait 60 etc/complement_jail_ldconfig.lock -c true || echo "[nvml] timed out waiting for the ldconfig lock holder"
    fi
    if ! lib_has_real_content "${so1}"; then
        # Grace re-checks: if the symlink becomes usable here, it was created and only this view lagged
        local waited=0
        for delay in 1 5; do
            sleep "${delay}"
            waited=$((waited + delay))
            if lib_has_real_content "${so1}"; then
                echo "[nvml] libnvidia-ml.so.1 usable after ${waited}s extra wait (view lag)"
                break
            fi
        done
    fi
    if ! lib_has_real_content "${so1}"; then
        # Repair: this worker's own view is verified healthy (mount_gpu_libs), so its ldconfig
        # produces correct symlinks no matter what the previous runner saw
        echo "[nvml] ANOMALY: libnvidia-ml.so.1 missing or empty after ldconfig phase (${how}), running ldconfig here"
        dump_nvml_state "before repair ldconfig (${how})"
        flock --wait 60 etc/complement_jail_ldconfig.lock -c "chroot \"${jaildir}\" /usr/sbin/ldconfig" || echo "[nvml] repair ldconfig failed with exit code $?"
        if ! lib_has_real_content "${so1}"; then
            echo "[nvml] libnvidia-ml.so.1 still missing or empty after repair, exiting so the container restarts"
            dump_nvml_state "final state before exit (${how})"
            exit 1
        fi
        echo "[nvml] libnvidia-ml.so.1 repaired by local ldconfig"
    else
        echo "[nvml] libnvidia-ml.so.1 usable after ldconfig phase (${how})"
    fi
    dump_nvml_state "after ldconfig phase (${how})"
}

# Mount points under the jail in this mount namespace (mountinfo field 5 is the mount point),
# as a sorted unique set ready for comm
jail_mount_points() {
    awk -v jail="${jaildir}/" 'index($5, jail) == 1 {print $5}' /proc/self/mountinfo | sort -u
}

# Run nvidia-container-cli once and verify its result. No retries by design: with the placeholders pre-created
# (see precreate_gpu_placeholders) the CLI creates nothing on the shared jail, so the concurrent-creation crash cannot
# happen, and if it still fails its cleanup cannot damage the other workers (it only deletes 0-byte mount targets).
# Whatever fails here is unexpected — dump diagnostics and exit; the kubelet container restart is the retry.
# Uses cap_args/FAKE_LDCONFIG set by the GPU section. Must be called with CWD = ${jaildir}.
mount_gpu_libs() {
    local mounts_before mounts_after mounted target
    mounts_before=$(jail_mount_points)
    if ! nvidia-container-cli \
        --user \
        --debug=/dev/stderr \
        --no-pivot \
        configure \
        --no-cgroups \
        --cuda-compat-mode=disabled \
        --ldconfig=$FAKE_LDCONFIG \
        --device=all \
        "${cap_args[@]}" \
        "${jaildir}"; then
        echo "[nvml] nvidia-container-cli failed, exiting so the container restarts"
        dump_nvml_state "after failed nvidia-container-cli"
        exit 1
    fi
    # Capability-agnostic verification: whatever set of files the enabled capabilities selected,
    # exactly the mounts the CLI just created must be attached and show real content by path
    mounts_after=$(jail_mount_points)
    mounted=$(comm -13 <(printf '%s\n' "${mounts_before}") <(printf '%s\n' "${mounts_after}"))
    if [ -z "${mounted}" ]; then
        echo "[nvml] ANOMALY: no new jail mounts after nvidia-container-cli, exiting so the container restarts"
        dump_nvml_state "after nvidia-container-cli without mounts"
        exit 1
    fi
    while IFS= read -r target; do
        # Only regular files carry content: directory mounts (driver procfs, app profiles)
        # and device/socket binds are attachment-only
        [ -f "${target}" ] || continue
        if ! lib_has_real_content "${target}"; then
            echo "[nvml] ANOMALY: ${target} shows no real content right after mounting, exiting so the container restarts"
            dump_nvml_state "after broken mount of ${target}"
            exit 1
        fi
    done <<< "${mounted}"
    echo "[nvml] GPU libs mounted, $(wc -l <<< "${mounted}" | tr -d ' ') new jail mounts verified"
}

# Pre-create a non-empty placeholder for every file nvidia-container-cli is going to bind-mount onto the shared jail.
# This removes both halves of the confirmed failure mechanism:
# - creation race: libnvidia-container's file_create() skips creation entirely (a single lstat, no create syscall)
#   when the target already exists with the host file's exact st_mode, so concurrent workers no longer race to create
#   the same files — the "file creation failed: ... file exists" CLI crash cannot happen;
# - cleanup damage: a failing CLI unmounts and DELETES every mount target of size 0
#   (nvc_mount.c unmount() -> file_remove()), unlinking the placeholders under the other
#   workers' live bind mounts cluster-wide. file_remove() spares non-empty files, so these
#   placeholders are immune no matter which worker's CLI fails, when, or why.
# Directories are deliberately not protected: the only directory the CLI mounts on the shared
# jail is etc/nvidia/nvidia-application-profiles-rc.d — an EGL-only profile dir shadowed by a
# per-node tmpfs; we pass --device=all, so losing it to a failing neighbor's cleanup (rmdir
# of an empty dir) is a no-op, and the next cold CLI run recreates it.
# Must be called with CWD = ${jaildir}.
readonly GPU_PLACEHOLDER_MARKER="soperator gpu placeholder"

# Where nvidia-container-cli will mount the given host file inside the jail.
# Libraries are flattened to the container's libs dir (nvc_mount.c mount_files: dst = libs_dir + basename)
# — e.g. host .../x86_64-linux-gnu/vdpau/libvdpau_nvidia.so.* is mounted at the flat lib path.
# Binaries are already flat and firmware keeps its full path, so both map 1:1.
# The libs dir here is hardcoded, not computed like the CLI does it: the CLI uses its compile-time
# per-arch constant /usr/lib/<arch>-linux-gnu because the jail contains /etc/debian_version
# (nvc_container.c, common.h USR_LIB_MULTIARCH_DIR) — equivalent to this expression for our
# Ubuntu jail on x86_64/aarch64, and it is the layout this whole script assumes everywhere.
# Must be called with CWD = ${jaildir}.
jail_mount_target() {
    local name
    name=$(basename "$1")
    # realpath resolves the jail's /lib -> usr/lib symlink; -m tolerates a missing tail.
    case "${name}" in
        lib*.so*) realpath -m "./usr/lib/${ALT_ARCH}-linux-gnu/${name}" ;;
        *)        realpath -m ".$1" ;;
    esac
}

precreate_gpu_placeholders() (
    # The () body runs the function in a subshell, so the umask change stays local to this call.
    # 0022 makes mkdir -p create 0755 directories on every component — same as the host dirs
    # and what the CLI's own make_ancestors produces (the CLI never compares directory modes)
    umask 0022
    local list_output host_path jail_path created lockfd
    local missing=()
    if ! list_output=$(nvidia-container-cli list); then
        echo "[nvml] nvidia-container-cli list failed, exiting so the container restarts"
        exit 1
    fi
    while IFS= read -r host_path; do
        [ -n "${host_path}" ] || continue
        jail_path=$(jail_mount_target "${host_path}")
        [ -e "${jail_path}" ] && continue
        missing+=("${host_path}")
    done <<< "${list_output}"

    if [ "${#missing[@]}" -eq 0 ]; then
        echo "[nvml] all GPU placeholders already present in the jail"
        return 0
    fi

    # Cold jail bring-up: create the missing placeholders under a cross-node lock so exactly one worker does it;
    # the others re-check under the lock and find everything present.
    echo "[nvml] ${#missing[@]} GPU placeholders missing, creating under lock"
    exec {lockfd}>etc/complement_jail_gpu_placeholders.lock
    if ! flock --wait 60 "${lockfd}"; then
        echo "[nvml] timed out waiting for the GPU placeholder lock, exiting so the container restarts"
        exit 1
    fi
    created=()
    for host_path in "${missing[@]}"; do
        jail_path=$(jail_mount_target "${host_path}")
        [ -e "${jail_path}" ] && continue  # created by a previous lock holder
        mkdir -p "$(dirname "${jail_path}")"
        # The write can fail spuriously (a possible EEXIST from O_CREAT on a stale view of the
        # shared FS, or a transient network-FS error) — tolerated: only the file's existence
        # matters, and the mount verification and ensure_nvml_soname judge the end state later.
        echo "${GPU_PLACEHOLDER_MARKER}" > "${jail_path}" || true
        # Exact host st_mode: file_create() skips creation only on full mode equality, and
        # the skip (a single lstat, no create syscall) is what removes the creation race
        chmod --reference="${host_path}" "${jail_path}" || true
        created+=("${jail_path}")
    done
    exec {lockfd}>&-  # releases the lock

    # The listing shows the marker size and the copied host mode of every file just created;
    # read-only, so done after releasing the lock to not keep the queued workers waiting
    echo "[nvml] pre-created ${#created[@]} GPU placeholders:"
    if [ "${#created[@]}" -gt 0 ]; then
        # shellcheck disable=SC2012 # human-readable log line, paths are known driver files
        ls -la "${created[@]}" | sed 's/^/[nvml]   /'
    fi
)

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

        # With the placeholders pre-created, running the CLI in parallel across
        # workers on the shared jail is safe (see precreate_gpu_placeholders)
        precreate_gpu_placeholders
        mount_gpu_libs
        touch "etc/gpu_libs_installed.flag"

        echo "[nvml] /lib symlink: $(readlink lib 2>/dev/null || echo 'NOT a symlink')"
        dump_nvml_state "after nvidia-container-cli"
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
        log "[gpu-wait] Bind-mount all GPU-specific lib placeholder files into the host's libdummy"
        # Placeholders are small marker files now (see precreate_gpu_placeholders), or 0-byte on
        # jails from before that change — match both by size; no real library is this small.
        # Only versioned lib names: the jail image ships other small files in this dir (glibc
        # stub .a archives, ncurses .so linker scripts) that must not be covered by libdummy
        find "${jaildir}/lib/${ALT_ARCH}-linux-gnu" -maxdepth 1 -type f ! -type l -size -64c -name 'lib*.so.*' -print0 | while IFS= read -r -d '' file; do
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
                    ensure_nvml_soname "failed"
                fi
                exit "$ldconfig_rc"
            fi
        fi
        rm -f "$current_cache_dump" "$candidate_cache_dump"
        if [ "$SLURM_CLUSTER_WITH_GPU" = "true" ]; then
            ensure_nvml_soname "$ldconfig_how"
        fi
    fi
popd
