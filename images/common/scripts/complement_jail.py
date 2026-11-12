#!/usr/bin/env python3
"""Complement the shared jail root with host and container pieces before Slurm starts.

The jail is one shared filesystem mounted by every login and worker node. Anything written to it
is visible cluster-wide, and several nodes run this script at the same time, so shared state is
created under cross-node locks and the steady state stays read-only.
"""

import argparse
import fcntl
import json
import os
import platform
import shlex
import signal
import stat
import subprocess
import sys
import time
from collections.abc import Callable, Iterable, Iterator
from contextlib import contextmanager
from dataclasses import dataclass
from datetime import datetime, timezone
from enum import Enum
from fnmatch import fnmatch
from itertools import repeat
from pathlib import Path, PurePosixPath
from typing import Literal

MOUNTINFO = Path("/proc/self/mountinfo")
LOG_PREFIX = "[complement_jail]"
# Placeholders are small marker files, no real library is this small
REAL_LIBRARY_MIN_SIZE = 1024
LOCK_TIMEOUT = 60
# Lock waits shorter than this are not worth a log line
LOCK_WAIT_WORTH_LOGGING = 1

# Shared with other script versions running on other nodes of the same jail: do not rename
PLACEHOLDER_MARKER = "soperator gpu placeholder\n"
DIVERT_SUBDIR = "soperator-diverted"
GPU_LIBS_FLAG = "etc/gpu_libs_installed.flag"

NVML_SONAME = "libnvidia-ml.so.1"
DRIVER_CAPABILITIES = ("compute", "compat32", "display", "graphics", "utility", "video")
# ldconfig is disabled in the CLI and run later under a cross-node lock to avoid races between workers
FAKE_LDCONFIG = "/usr/bin/true"

SLURM_PLUGINS = ("chroot.so", "spanknccldebug.so", "spank_nccl_inspector_preconf.so")
ENROOT_CAPABILITIES = {"enroot-mksquashovlfs": "cap_sys_admin+pe", "enroot-aufs2ovlfs": "cap_sys_admin,cap_mknod+pe"}
TASK_PROLOG_FSTAB = Path("/etc/enroot/mounts.d/task_prolog.fstab")
TASK_PROLOG_FSTAB_LINE = (
    "/opt/slurm_scripts/task_prolog.sh /opt/slurm_scripts/task_prolog.sh none"
    " x-create=file,bind,ro,nosuid,nodev,private,nofail 0 0\n"
)

# local/ is node-local (a bind of the worker's boot disk, or a plain dir on login nodes), shared/ stays on the jail FS.
# World-writable because jobs, SPANK plugins and health checks write there as arbitrary users.
OUTPUT_DIRS = (
    "opt/soperator-outputs",
    "opt/soperator-outputs/local",
    "opt/soperator-outputs/local/nccl_logs",
    "opt/soperator-outputs/local/slurm_jobs",
    "opt/soperator-outputs/local/slurm_scripts",
    "opt/soperator-outputs/local/task_prolog",
    "opt/soperator-outputs/local/health_checker_cmd_stdout",
    "opt/soperator-outputs/shared",
    "opt/soperator-outputs/shared/nccl_profiles",
    "opt/soperator-outputs/shared/acceptance",
)

GPU_WAIT_INTERVAL = 10
# Placeholders are small marker files (or 0-byte on older jails); no real library is this small
PLACEHOLDER_MAX_SIZE = 64

DOCKER_DAEMON_CONFIG = Path("/etc/docker/daemon.json")
DOCKER_DAEMON_CONFIG_FOR_CPU = Path("/run/soperator-docker-daemon.json")
DOCKER_DISABLED_STUB = Path("/opt/soperator-docker-disabled.sh")
DOCKER_DISABLED_MESSAGE = (
    "docker is not available on this cluster: it was deployed without image-storage disks required for storing Docker data"
)


@dataclass(frozen=True)
class Config:
    jail_dir: Path
    upper_dir: Path
    worker: bool
    gpu_enabled: bool
    cluster_with_gpu: bool
    docker_enabled: bool
    driver_capabilities: str
    arch: str

    @classmethod
    def from_args(cls, argv=None, environ=os.environ):
        parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
        parser.add_argument("-j", "--jail-dir", required=True, type=Path, help="jail root directory")
        parser.add_argument("-u", "--upper-dir", required=True, type=Path, help="upper jail directory whose submounts are bound into the jail")
        parser.add_argument("-w", "--worker", action="store_true", help="this is a worker node (a login node otherwise)")
        parser.add_argument("--arch", default=platform.machine(), help="architecture of the multiarch library dir (default: this machine's)")
        args = parser.parse_args(argv)
        return cls(
            jail_dir=args.jail_dir,
            upper_dir=args.upper_dir,
            worker=args.worker,
            gpu_enabled=environ.get("NODESET_GPU_ENABLED") == "true",
            cluster_with_gpu=environ.get("SLURM_CLUSTER_WITH_GPU") == "true",
            docker_enabled=environ.get("SOPERATOR_DOCKER_ENABLED", "true") == "true",
            driver_capabilities=environ.get("NVIDIA_DRIVER_CAPABILITIES") or "compute,utility",
            arch=args.arch,
        )

    @property
    def provisions_gpu_libs(self):
        """GPU workers mount the host driver into the shared jail for everyone."""
        return self.worker and self.gpu_enabled

    @property
    def covers_gpu_placeholders(self):
        """Every other node of a GPU cluster covers those mounts' placeholders with libdummy."""
        return self.cluster_with_gpu and not self.provisions_gpu_libs

    @property
    def refreshes_linker_cache(self):
        """Workers rebuild the shared linker cache, except CPU workers in GPU clusters: they see libdummy
        (no SONAME) over the placeholders, so their view never matches a cache built by a GPU worker and
        letting both rebuild would make the cache flip on every worker start."""
        return self.worker and not self.covers_gpu_placeholders


class LockTimeout(Exception):
    pass


class Fatal(Exception):
    """Abort the entry point; the kubelet restarting the container is the retry."""

    def __init__(self, tag, message, exit_code=1):
        self.tag = tag
        self.text = f"{message}, exiting so the container restarts"
        self.exit_code = exit_code
        super().__init__(f"{tag} {self.text}")


@dataclass(frozen=True)
class MountTarget:
    host: str
    jail: Path


class LdconfigOutcome(Enum):
    RAN = "ran"
    LOCK_BUSY = "lock-busy"
    SKIPPED = "skipped"
    FAILED = "failed"


@dataclass(frozen=True)
class LinkerCacheUpdate:
    outcome: LdconfigOutcome
    exit_code: int = 0


@dataclass(frozen=True)
class Jail:
    root: Path
    arch: str

    def path(self, relative) -> Path:
        return self.root / relative

    def inside(self, path: Path) -> str:
        """The jail-absolute spelling of a path below the jail root, as chroot'ed tools see it."""
        return "/" + path.relative_to(self.root).as_posix()

    @property
    def lib_dir(self) -> Path:
        """The multiarch library dir every driver library is flattened into."""
        return self.root / f"usr/lib/{self.arch}-linux-gnu"

    def chroot_run(self, argv, **kwargs):
        return run(["chroot", self.root, *argv], **kwargs)

    @contextmanager
    def lock(self, name, *, timeout) -> Iterator[bool]:
        """flock on a file in the shared jail, so it is held across all nodes.

        Yields whether the lock was acquired within timeout seconds (0 means do not wait).
        The file names are shared with older script versions running on other nodes.
        """
        lock_path = self.path(f"etc/complement_jail_{name}.lock")
        with lock_path.open("a") as lock_file:
            started = time.monotonic()
            acquired = self._acquire(lock_file, timeout)
            waited = time.monotonic() - started
            if acquired and waited >= LOCK_WAIT_WORTH_LOGGING:
                log("[lock]", f"acquired the {name} lock after waiting {waited:.1f}s")
            try:
                yield acquired
            finally:
                if acquired:
                    fcntl.flock(lock_file, fcntl.LOCK_UN)

    def ldconfig(self, *args, **kwargs):
        return self.chroot_run(["/usr/sbin/ldconfig", *args], **kwargs)

    def ldconfig_entries(self, cache=None) -> list[str]:
        """Sorted library lines of a linker cache; the header line, which embeds the cache path, is dropped."""
        args = ["-C", cache] if cache else []
        listing = self.ldconfig(*args, "-p", capture=True, quiet_stderr=True).stdout
        return sorted(line for line in listing.splitlines() if "=>" in line)

    def diversions(self) -> set[tuple[str, str]]:
        """(diverted path, divert-to) pairs from dpkg's database: three lines per entry, the third is the package."""
        database = self.path("var/lib/dpkg/diversions")
        if not database.exists():
            return set()
        lines = database.read_text().splitlines()
        return {(lines[i], lines[i + 1]) for i in range(0, len(lines) - 2, 3)}

    def add_diversion(self, source, target):
        self.chroot_run(["/usr/bin/dpkg-divert", "--local", "--no-rename", "--divert", target, "--add", source])

    @contextmanager
    def dpkg_locks(self) -> Iterator[bool]:
        """dpkg's own lock files, taken the way apt and dpkg take them (POSIX locks), without waiting.

        Yields whether they were free; holding them keeps every package operation on every node out.
        """
        locks = [self.path(f"var/lib/dpkg/{name}").open("a") for name in ("lock-frontend", "lock")]
        try:
            try:
                for lock in locks:
                    fcntl.lockf(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
            except BlockingIOError:
                yield False
                return
            yield True
        finally:
            for lock in locks:
                lock.close()

    @staticmethod
    def _acquire(lock_file, timeout):
        """A blocking flock that gives up after timeout seconds, like `flock --wait`: SIGALRM
        interrupts the wait. A timeout of 0 tries once without waiting."""
        if timeout == 0:
            try:
                fcntl.flock(lock_file, fcntl.LOCK_EX | fcntl.LOCK_NB)
                return True
            except BlockingIOError:
                return False

        def on_alarm(signum, frame):
            raise LockTimeout

        previous_handler = signal.signal(signal.SIGALRM, on_alarm)
        acquired = False
        try:
            try:
                signal.setitimer(signal.ITIMER_REAL, timeout)
                fcntl.flock(lock_file, fcntl.LOCK_EX)
                acquired = True
            finally:
                signal.setitimer(signal.ITIMER_REAL, 0)
        except LockTimeout:
            pass  # an alarm that fires right after the lock was taken still leaves it acquired
        finally:
            signal.signal(signal.SIGALRM, previous_handler)
        return acquired


def main(config: Config):
    jail = Jail(config.jail_dir.resolve(), config.arch)
    os.umask(0o022)  # placeholders need 0755 parent dirs, same as the host and the CLI create
    log("[arch]", f"Using ALT_ARCH = {config.arch}")

    mount_virtual_filesystems(jail)
    mount_host_configs(jail)
    mount_jail_submounts(jail, config.upper_dir)
    gpu_targets = provision_gpu_libs(jail, config.driver_capabilities) if config.provisions_gpu_libs else []
    bind_slurm_client(jail)
    bind_enroot(jail)
    bind_pyxis(jail)
    create_output_dirs(jail)
    if config.covers_gpu_placeholders:
        cover_gpu_placeholders_with_libdummy(jail)
    if config.worker:
        bind_slurmd(jail)
        configure_docker(jail, config)
    if config.refreshes_linker_cache:
        refresh_linker_cache(jail, config)
    if gpu_targets:
        divert_gpu_sonames(jail, gpu_targets)


def log(tag, message):
    print(f"{timestamp()} {LOG_PREFIX} {tag} {message}", flush=True)


def timestamp():
    now = datetime.now(timezone.utc)
    return f"[{now:%Y-%m-%dT%H:%M:%S}.{now.microsecond // 1000:03d}]"


def mount_virtual_filesystems(jail: Jail):
    log("[virtual-fs]", "Bind-mount virtual filesystems, /tmp and the node-local /var/log")
    mount_fs("proc", "/proc", jail.path("proc"))
    mount_fs("sysfs", "/sys", jail.path("sys"))
    bind_mount("/dev", jail.path("dev"), recursive=True)
    bind_mount("/run", jail.path("run"), recursive=True)
    bind_mount("/sys/fs/cgroup", jail.path("sys/fs/cgroup"), recursive=True)
    bind_mount("/tmp", jail.path("tmp"))
    bind_mount("/var/log", jail.path("var/log"))


def mount_fs(fstype, source, target: Path):
    run(["mount", "-t", fstype, source, target])


def run(argv, *, check=True, capture=False, quiet_stderr=False):
    """Run a command, tracing it with a timestamp so concurrent nodes can be correlated in logs."""
    argv = [str(arg) for arg in argv]
    trace(shlex.join(argv))
    return subprocess.run(
        argv,
        check=check,
        text=True,
        stdout=subprocess.PIPE if capture else None,
        stderr=subprocess.DEVNULL if quiet_stderr else None,
    )


def trace(command):
    print(f"{timestamp()} {LOG_PREFIX} + {command}", file=sys.stderr, flush=True)


def bind_mount(source, target: Path, *, recursive=False, create: Literal["file", "dir", None] = None):
    """Bind-mount source onto target, creating the mount point first when `create` says of which kind."""
    if create == "file":
        ensure_file(target)
    elif create == "dir":
        ensure_dir(target)
    run(["mount", "--rbind" if recursive else "--bind", source, target])


def ensure_file(path: Path):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.touch(exist_ok=True)


def ensure_dir(path: Path, mode=None):
    path.mkdir(parents=True, exist_ok=True)
    if mode is not None:
        chmod_if_differs(path, mode)


def chmod_if_differs(path: Path, mode):
    """An unconditional chmod bumps ctime even when nothing changes; on the shared jail every node
    would do that on every start."""
    if stat.S_IMODE(path.stat().st_mode) != mode:
        path.chmod(mode)


def mount_host_configs(jail: Jail):
    log("[dns]", "Bind-mount DNS configuration")
    bind_mount("/etc/resolv.conf", jail.path("etc/resolv.conf"))
    bind_mount("/etc/hosts", jail.path("etc/hosts"))
    log("[sssd]", "Bind-mount SSSD configuration and sockets if they exist")
    if Path("/etc/sssd/sssd.conf").is_file():
        bind_mount("/etc/sssd", jail.path("etc/sssd"))
    if Path("/var/lib/sss/pipes").is_dir():
        bind_mount("/var/lib/sss/pipes", jail.path("var/lib/sss/pipes"), create="dir")


def mount_jail_submounts(jail: Jail, upper_dir: Path):
    """Bind the volumes mounted below the upper jail dir (e.g. /home) at the same place in the jail."""
    log("[submounts]", f"Bind-mount jail submounts from upper {upper_dir} into the actual {jail.root}")
    listing = output(["findmnt", "--output", "TARGET", "--submounts", "--target", "/", "--pairs"])
    for line in listing.splitlines():
        submount = Path(line.removeprefix('TARGET="').removesuffix('"'))
        if submount == upper_dir or not submount.is_relative_to(upper_dir):
            continue
        target = jail.path(submount.relative_to(upper_dir))
        log("[submounts]", f"Bind-mount jail submount {target}")
        if submount.is_dir():
            bind_mount(submount, target, recursive=True, create="dir")
        elif submount.is_file():
            bind_mount(submount, target, create="file")


def output(argv, **kwargs):
    return run(argv, capture=True, **kwargs).stdout


def provision_gpu_libs(jail: Jail, driver_capabilities) -> list[MountTarget]:
    """Mount the host's NVIDIA driver files into the jail and return what was mounted."""
    log("[gpu]", "Run nvidia-container-cli to propagate NVIDIA drivers, CUDA, NVML and other GPU-related stuff to the jail")
    capability_flags = driver_capability_flags(driver_capabilities)
    jail_mount = output(["findmnt", "--target", jail.root, "--output", "SOURCE,FSTYPE,OPTIONS", "--noheadings"], check=False)
    log("[nvml]", f"Jail mount: {jail_mount.strip()}")
    targets = gpu_mount_targets(jail)
    precreate_gpu_placeholders(jail, targets)
    mount_gpu_libs(jail, capability_flags)
    ensure_file(jail.path(GPU_LIBS_FLAG))
    divert_gpu_mount_targets(jail, targets)
    lib = jail.path("lib")
    log("[nvml]", f"/lib symlink: {os.readlink(lib) if lib.is_symlink() else 'NOT a symlink'}")
    dump_nvml_state(jail, "after nvidia-container-cli")
    return targets


def driver_capability_flags(spec):
    """CLI flags for a NVIDIA_DRIVER_CAPABILITIES value; `all` expands to every capability."""
    flags = []
    for capability in filter(None, (part.strip() for part in spec.split(","))):
        if capability == "all":
            return [f"--{name}" for name in DRIVER_CAPABILITIES]
        if capability not in DRIVER_CAPABILITIES:
            raise Fatal("[gpu]", f"Unknown NVIDIA driver capability: {capability}")
        flags.append(f"--{capability}")
    return flags


def gpu_mount_targets(jail: Jail) -> list[MountTarget]:
    """Every file nvidia-container-cli is going to mount, with its target inside the jail."""
    try:
        listing = output(["nvidia-container-cli", "list"])
    except subprocess.CalledProcessError:
        fatal("[nvml]", "nvidia-container-cli list failed")
    targets = [MountTarget(host, jail_mount_target(jail, host)) for host in listing.splitlines() if host]
    log("[nvml]", f"nvidia-container-cli lists {len(targets)} driver files:")
    for target in targets:
        log("[nvml]", f"  {target.host} -> {target.jail}")
    return targets


def jail_mount_target(jail: Jail, host_path) -> Path:
    """Where nvidia-container-cli mounts the given host file inside the jail.

    Libraries are flattened into the multiarch lib dir (nvc_mount.c mount_files: dst = libs_dir +
    basename), so host .../vdpau/libvdpau_nvidia.so.* lands at the flat path. Binaries and firmware
    keep their path. Symlinks in the jail (e.g. /lib -> usr/lib) are resolved like the kernel will.
    """
    name = PurePosixPath(host_path).name
    if fnmatch(name, "lib*.so*"):
        return (jail.lib_dir / name).resolve()
    return jail.path(host_path.lstrip("/")).resolve()


def fatal(tag, message, exit_code=1):
    raise Fatal(tag, message, exit_code)


def precreate_gpu_placeholders(jail: Jail, targets: Iterable[MountTarget]):
    """Pre-create a non-empty placeholder for every file nvidia-container-cli is going to mount.

    Several workers run the CLI concurrently, and a failing CLI deletes every 0-byte mount target
    it created (nvc_mount.c unmount() -> file_remove()), which would unlink the files under other
    workers' live mounts. Placeholders with the host file's exact mode remove both problems: the
    CLI skips creation (a single lstat, no race) and its cleanup spares non-empty files.
    """
    missing = [target for target in targets if not target.jail.exists()]
    if not missing:
        log("[nvml]", "all GPU placeholders already present in the jail")
        return
    log("[nvml]", f"{len(missing)} GPU placeholders missing, creating under lock")
    with jail.lock("gpu_placeholders", timeout=LOCK_TIMEOUT) as acquired:
        if not acquired:
            fatal("[nvml]", "timed out waiting for the GPU placeholder lock")
        created = [target.jail for target in missing if create_placeholder(target)]
    log("[nvml]", f"pre-created {len(created)} GPU placeholders:")
    for path in created:
        info = path.lstat()
        log("[nvml]", f"  {stat.filemode(info.st_mode)} {info.st_size} {path}")


def create_placeholder(target: MountTarget):
    """Exclusive create, so a placeholder another worker made meanwhile is never truncated: a 0-byte
    moment is what a failing CLI's cleanup deletes. Returns whether this call created it."""
    target.jail.parent.mkdir(parents=True, exist_ok=True)
    try:
        with target.jail.open("x") as placeholder:
            placeholder.write(PLACEHOLDER_MARKER)
    except FileExistsError:
        return False
    except OSError:
        pass  # other write errors are tolerated: only existence matters, the mount verification judges the end state
    # The exact host mode is what makes the CLI skip creation instead of racing for it
    try:
        target.jail.chmod(stat.S_IMODE(os.stat(target.host).st_mode))
    except OSError:
        pass
    return True


def write_file(path: Path, content, mode=None):
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(content)
    if mode is not None:
        path.chmod(mode)


def mount_gpu_libs(jail: Jail, capability_flags):
    """Run nvidia-container-cli once and verify its mounts. No retries: with the placeholders in
    place the CLI creates nothing on the shared jail, so whatever fails here is unexpected."""
    before = mount_points_under(jail.root)
    result = run(
        [
            "nvidia-container-cli", "--user", "--debug=/dev/stderr", "--no-pivot",
            "configure", "--no-cgroups", "--cuda-compat-mode=disabled", f"--ldconfig={FAKE_LDCONFIG}",
            "--device=all", *capability_flags, jail.root,
        ],
        check=False,
    )
    if result.returncode != 0:
        dump_nvml_state(jail, "after failed nvidia-container-cli")
        fatal("[nvml]", "nvidia-container-cli failed")
    mounted = mount_points_under(jail.root) - before
    if not mounted:
        dump_nvml_state(jail, "after nvidia-container-cli without mounts")
        fatal("[nvml]", "ANOMALY: no new jail mounts after nvidia-container-cli")
    for target in sorted(mounted):
        # Only regular files carry content: directory, device and socket binds are attachment-only
        if target.is_file() and not has_real_content(target):
            dump_nvml_state(jail, f"after broken mount of {target}")
            fatal("[nvml]", f"ANOMALY: {target} shows no real content right after mounting")
    log("[nvml]", f"GPU libs mounted, {len(mounted)} new jail mounts verified")


def mount_points_under(prefix: Path, mountinfo: Path = MOUNTINFO) -> set[Path]:
    """Mount points strictly below prefix in this mount namespace."""
    points = set()
    for line in mountinfo.read_text().splitlines():
        point = Path(line.split()[4].replace("\\040", " "))
        if prefix in point.parents:
            points.add(point)
    return points


def dump_nvml_state(jail: Jail, label):
    """NVML diagnostics, [nvml]-prefixed for grepping. A difference between the path view and the
    chroot view means ldconfig resolves the jail through another view; healthy-looking libraries
    with 0 mounts mean the CLI's mounts landed in another namespace."""
    log("[nvml]", f"=== NVML jail state: {label} ===")
    log("[nvml]", "--- path view:")
    for entry in sorted(jail.lib_dir.iterdir()) if jail.lib_dir.is_dir() else []:
        if _is_nvml_entry(entry.name):
            info = entry.lstat()
            log("[nvml]", f"  {info.st_ino} {stat.filemode(info.st_mode)} {info.st_size} {entry.name}")
    log("[nvml]", "--- chroot view (what ldconfig sees):")
    listing = jail.chroot_run(["ls", "-lai", jail.inside(jail.lib_dir) + "/"], capture=True, check=False).stdout
    for line in listing.splitlines():
        if _is_nvml_entry(line):
            log("[nvml]", f"  {line}")
    mounts = mount_points_under(jail.lib_dir)
    nvidia = sum(point.name.startswith("libnvidia") for point in mounts)
    nvml = sum(point.name.startswith("libnvidia-ml") for point in mounts)
    log("[nvml]", f"--- nvidia lib bind mounts in this namespace: {nvidia} (libnvidia-ml: {nvml})")
    # The CLI derives its target namespace from ${jail}/proc/<parent pid>/ns/mnt; the two must agree
    log("[nvml]", f"--- mount namespace self: {mount_namespace()} via jail proc: {mount_namespace(jail)}")


def mount_namespace(jail: Jail | None = None):
    """This process's mount namespace id, read through the jail's proc when given."""
    link = f"proc/{os.getpid()}/ns/mnt"
    try:
        return os.readlink(jail.path(link) if jail else "/" + link)
    except OSError as error:
        return str(error)
    log("[nvml]", f"=== end NVML jail state: {label} ===")


def _is_nvml_entry(text):
    return "libnvidia-ml" in text or "libcuda.so" in text


def has_real_content(path: Path):
    """Whether the file (following symlinks) is bigger than any placeholder or marker file."""
    try:
        return path.stat().st_size > REAL_LIBRARY_MIN_SIZE
    except OSError:
        return False


def divert_gpu_mount_targets(jail: Jail, targets: Iterable[MountTarget]):
    """Divert every regular file mounted by nvidia-container-cli."""
    paths = []
    for target in targets:
        if not target.jail.is_file():
            continue
        inside = jail.inside(target.jail)
        paths.append(inside)
        # dpkg matches the literal packaged path, so a firmware file listed as /lib/... but mounted
        # through the jail's /lib -> usr/lib symlink is diverted under both spellings
        if not fnmatch(PurePosixPath(target.host).name, "lib*.so*") and target.host != inside:
            paths.append(target.host)
    divert_jail_paths(jail, paths)


def divert_jail_paths(jail: Jail, paths: Iterable[str]):
    """Add the missing local dpkg diversions for jail-absolute paths.

    dpkg installs and removes files by renaming over the target, which fails on a bind mount and
    would replace the shared soname symlinks; a diversion makes dpkg use the divert-to path instead.
    It only redirects what dpkg places: ldconfig run by a postinst can still repoint a soname
    symlink to a newer packaged library. dpkg-divert takes no lock and rewrites the diversions file
    wholesale, so adding runs under a cross-node flock and dpkg's own locks. Non-fatal: the jail
    works without diversions.
    """
    wanted = {(path, divert_target(path)) for path in paths}
    missing = missing_diversions(jail, wanted)
    if not missing:
        log("[divert]", "all dpkg diversions present")
        return
    log("[divert]", f"adding {len(missing)} dpkg diversions under lock")
    with jail.lock("dpkg_diverts", timeout=LOCK_TIMEOUT) as acquired:
        if not acquired:
            log("[divert]", "timed out waiting for the dpkg diversions lock, skipping")
            return
        missing = missing_diversions(jail, wanted)  # another worker usually added them while we waited
        if not missing:
            log("[divert]", "all dpkg diversions added by another worker meanwhile")
            return
        with jail.dpkg_locks() as dpkg_idle:
            if not dpkg_idle:
                log("[divert]", "dpkg database is in use (a package operation is running), skipping until the next start")
                return
            for source, target in missing:
                ensure_dir(jail.path(target.lstrip("/")).parent)  # dpkg does not create it
                try:
                    jail.add_diversion(source, target)
                except subprocess.CalledProcessError as error:
                    log("[divert]", f"ANOMALY: diverting {source} failed with exit code {error.returncode}")


def missing_diversions(jail: Jail, wanted):
    return sorted(wanted - jail.diversions())


def divert_target(path):
    """A subdirectory next to the file, not dpkg's default .distrib suffix: ldconfig would rank such
    a lib*.so* sibling above the mounted library when choosing the soname symlink target."""
    path = PurePosixPath(path)
    return str(path.parent / DIVERT_SUBDIR / path.name)


def bind_slurm_client(jail: Jail):
    log("[slurm]", "Bind-mount the Slurm client and plugins from the container")
    run(["/opt/bin/slurm/bind_slurm_common.sh", "-j", jail.root])
    for plugin in SLURM_PLUGINS:
        bind_mount(slurm_plugin_path(jail.arch, plugin), jail.path(f"usr/lib/slurm/{plugin}"), create="file")


def slurm_plugin_path(arch, name):
    return f"/usr/lib/{arch}-linux-gnu/slurm/{name}"


def bind_enroot(jail: Jail):
    log("[enroot]", "Bind-mount enroot configuration, data, libraries and binaries from the container")
    write_file(TASK_PROLOG_FSTAB, TASK_PROLOG_FSTAB_LINE)
    bind_mount("/etc/enroot", jail.path("etc/enroot"), recursive=True, create="dir")
    bind_mount("/usr/share/enroot", jail.path("usr/share/enroot"), create="dir")
    bind_mount("/usr/lib/enroot", jail.path("usr/lib/enroot"), create="dir")
    for binary in sorted(Path("/usr/bin").glob("enroot*")):
        bind_mount(binary, jail.path(f"usr/bin/{binary.name}"), create="file")
    for binary, capabilities in ENROOT_CAPABILITIES.items():
        grant_capabilities(jail, binary, capabilities)
    log("[enroot]", "Create shared directories for caching Pyxis sqshfs files and Enroot OCI data")
    for cache in ("var/cache/enroot-container-images", "var/cache/enroot"):
        ensure_dir(jail.path(cache), mode=0o1777)


def grant_capabilities(jail: Jail, binary, capabilities):
    """setcap on the shared jail: only when missing, and under a lock against the other nodes."""
    path = jail.path(f"usr/bin/{binary}")
    if capabilities in output(["getcap", path], check=False):
        return
    log("[enroot]", f"Set capabilities for {binary} to run containers without privileges")
    with jail.lock(f"setcap_{binary.replace('-', '_')}", timeout=LOCK_TIMEOUT) as acquired:
        if not acquired:
            fatal("[enroot]", f"timed out waiting for the setcap lock of {binary}")
        run(["setcap", capabilities, path])


def bind_pyxis(jail: Jail):
    log("[pyxis]", "Bind-mount the pyxis plugin from the container")
    bind_mount(slurm_plugin_path(jail.arch, "spank_pyxis.so"), jail.path("usr/lib/slurm/spank_pyxis.so"), create="file")


def create_output_dirs(jail: Jail):
    log("[outputs]", "Create Soperator output directories")
    for rel in OUTPUT_DIRS:
        ensure_dir(jail.path(rel), mode=0o777)


def cover_gpu_placeholders_with_libdummy(jail: Jail):
    """Nodes without the driver (login nodes, CPU workers in GPU clusters) bind a dummy library over
    every placeholder, so NVML consumers get a clean failure instead of an empty file."""
    wait_until(
        jail.path(GPU_LIBS_FLAG).exists,
        repeat(GPU_WAIT_INTERVAL),
        waiting=("[gpu-wait]", "Waiting for GPU libs to be propagated to the jail from a worker node"),
    )
    log("[gpu-wait]", "Bind-mount all GPU-specific lib placeholder files into the host's libdummy")
    with jail.lock("gpu_placeholders", timeout=LOCK_TIMEOUT) as acquired:
        if not acquired:
            fatal("[gpu-wait]", "timed out waiting for the GPU placeholder lock")
        placeholders = gpu_placeholders(jail)  # under the lock, so a batch being created is seen whole
    for placeholder in placeholders:
        bind_mount(f"/lib/{jail.arch}-linux-gnu/libdummy.so", placeholder)


def wait_until(condition: Callable[[], bool], delays: Iterable[float], *, waiting=None):
    """Re-check condition after each delay, logging `waiting` (a tag and a message) before every pause.

    Returns the seconds waited until the condition held, or None when the delays ran out.
    """
    if condition():
        return 0
    waited = 0
    for delay in delays:
        if waiting:
            log(*waiting)
        time.sleep(delay)
        waited += delay
        if condition():
            return waited
    return None


def gpu_placeholders(jail: Jail) -> list[Path]:
    """Versioned library names only: the jail ships other small files (glibc stub .a archives,
    ncurses .so linker scripts) that must not be covered."""
    return sorted(
        path
        for path in jail.lib_dir.glob("lib*.so.*")
        if path.is_file() and not path.is_symlink() and path.stat().st_size < PLACEHOLDER_MAX_SIZE
    )


def bind_slurmd(jail: Jail):
    log("[slurmd]", "Bind-mount the slurmd spool directory from the host and slurmd binaries from the container")
    bind_mount("/var/spool/slurmd", jail.path("var/spool/slurmd"))
    for binary in ("slurmd", "slurmstepd"):
        bind_mount(f"/usr/sbin/{binary}", jail.path(f"usr/sbin/{binary}"), create="file")


def configure_docker(jail: Jail, config: Config):
    if not config.docker_enabled:
        mask_docker(jail)
        return
    log("[docker]", "Bind-mount the dockerd configuration from the container")
    daemon_config = DOCKER_DAEMON_CONFIG
    if not config.gpu_enabled:
        daemon_config = docker_config_without_nvidia_runtime()
    bind_mount(daemon_config, jail.path("etc/docker/daemon.json"), create="file")


def mask_docker(jail: Jail):
    log("[docker]", "Docker is disabled, masking docker binaries in the jail")
    write_file(DOCKER_DISABLED_STUB, f"#!/bin/sh\necho {shlex.quote(DOCKER_DISABLED_MESSAGE)} >&2\nexit 1\n", mode=0o755)
    for binary in ("docker", "dockerd"):
        target = jail.path(f"usr/bin/{binary}")
        if target.is_file():
            bind_mount(DOCKER_DISABLED_STUB, target)


def docker_config_without_nvidia_runtime() -> Path:
    """CPU workers in GPU clusters see dummy NVML, which crashes the NVIDIA runtime auto detection.
    The source config can be a read-only ConfigMap mount, so the rewritten copy is node-local."""
    settings = json.loads(DOCKER_DAEMON_CONFIG.read_text())
    if settings.get("default-runtime") == "nvidia":
        settings["default-runtime"] = "runc"
    write_file(DOCKER_DAEMON_CONFIG_FOR_CPU, json.dumps(settings, indent=2) + "\n")
    return DOCKER_DAEMON_CONFIG_FOR_CPU


def refresh_linker_cache(jail: Jail, config: Config):
    """Rebuild the shared ld.so.cache when stale, and make sure NVML resolves afterwards."""
    if config.cluster_with_gpu:
        dump_nvml_state(jail, "before ldconfig")
    update = update_linker_cache(jail)
    if config.cluster_with_gpu:
        ensure_nvml_soname(jail, update.outcome)
    if update.outcome is LdconfigOutcome.FAILED:
        fatal("[ldcache]", "the linker cache could not be rebuilt", exit_code=update.exit_code)


def update_linker_cache(jail: Jail) -> LinkerCacheUpdate:
    """Rebuild the jail's ld.so.cache only when its contents would change.

    The cache lives in the shared jail, so rebuilding it briefly leaves every running job on every
    node without one. The probe builds a throwaway cache from the libraries currently visible (-X:
    no symlinks, -i: no shared aux-cache) in the node-local /tmp and compares the entries.
    """
    log("[ldcache]", "Probing whether the linker cache is up to date")
    current = jail.ldconfig_entries()
    candidate = f"/tmp/ld.so.cache.candidate.{os.getpid()}"
    jail.ldconfig("-X", "-i", "-C", candidate)
    try:
        wanted = jail.ldconfig_entries(candidate)
    finally:
        jail.path(candidate.lstrip("/")).unlink(missing_ok=True)
    if current == wanted:
        log("[ldcache]", "Linker cache is already up to date, skipping ldconfig")
        return LinkerCacheUpdate(LdconfigOutcome.SKIPPED)

    log("[ldcache]", "Linker cache is stale, updating it inside the jail")
    with jail.lock("ldconfig", timeout=0) as acquired:
        if not acquired:
            log("[ldcache]", "ldconfig skipped: another worker holds the lock and is rebuilding the cache")
            return LinkerCacheUpdate(LdconfigOutcome.LOCK_BUSY)
        result = jail.ldconfig(check=False)
    if result.returncode == 0:
        log("[ldcache]", "ldconfig completed successfully (got flock)")
        return LinkerCacheUpdate(LdconfigOutcome.RAN)
    log("[ldcache]", f"ldconfig FAILED with exit code {result.returncode}")
    return LinkerCacheUpdate(LdconfigOutcome.FAILED, result.returncode)


def ensure_nvml_soname(jail: Jail, outcome: LdconfigOutcome):
    """Make sure the NVML soname symlink points at real content after the ldconfig phase, repairing
    with a local ldconfig; a jail that still has no working NVML restarts the container."""
    soname = jail.lib_dir / NVML_SONAME
    how = outcome.value
    # A GPU worker whose capability set excludes `utility` gets no NVML by design
    if not nvml_mounted(jail):
        log("[nvml]", "libnvidia-ml not mounted (capability set excludes utility), skipping the soname check")
        return
    if outcome is LdconfigOutcome.LOCK_BUSY and not has_real_content(soname):
        # The lock is released exactly when the holder's ldconfig exits
        with jail.lock("ldconfig", timeout=LOCK_TIMEOUT) as acquired:
            if not acquired:
                log("[nvml]", "timed out waiting for the ldconfig lock holder")
    if not has_real_content(soname):
        waited = wait_until(lambda: has_real_content(soname), [1, 5])
        if waited:
            log("[nvml]", f"{NVML_SONAME} usable after {waited}s extra wait (view lag)")
    if has_real_content(soname):
        log("[nvml]", f"{NVML_SONAME} usable after ldconfig phase ({how})")
    else:
        # This worker's own view is verified healthy, so its ldconfig produces correct symlinks
        log("[nvml]", f"ANOMALY: {NVML_SONAME} missing or empty after ldconfig phase ({how}), running ldconfig here")
        dump_nvml_state(jail, f"before repair ldconfig ({how})")
        _repair_ldconfig(jail)
        if not has_real_content(soname):
            dump_nvml_state(jail, f"final state before exit ({how})")
            fatal("[nvml]", f"{NVML_SONAME} still missing or empty after repair")
        log("[nvml]", f"{NVML_SONAME} repaired by local ldconfig")
    dump_nvml_state(jail, f"after ldconfig phase ({how})")


def nvml_mounted(jail: Jail):
    return any(point.name.startswith("libnvidia-ml") for point in mount_points_under(jail.lib_dir))


def _repair_ldconfig(jail: Jail):
    with jail.lock("ldconfig", timeout=LOCK_TIMEOUT) as acquired:
        if not acquired:
            log("[nvml]", "repair ldconfig skipped: timed out waiting for the ldconfig lock")
            return
        result = jail.ldconfig(check=False)
    if result.returncode != 0:
        log("[nvml]", f"repair ldconfig failed with exit code {result.returncode}")


def divert_gpu_sonames(jail: Jail, targets: Iterable[MountTarget]):
    """Divert the soname symlinks ldconfig created for the mounted libraries, e.g. libnvidia-ml.so.1."""
    mounted = {target.jail for target in targets}
    links = [link for link in sorted(jail.lib_dir.glob("lib*.so*")) if link.is_symlink() and link.resolve() in mounted]
    divert_jail_paths(jail, [jail.inside(link) for link in links])


if __name__ == "__main__":
    try:
        main(Config.from_args())
    except Fatal as error:
        log(error.tag, error.text)
        sys.exit(error.exit_code)
    except subprocess.CalledProcessError as error:
        log("[fatal]", str(error))
        sys.exit(error.returncode)
