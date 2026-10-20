import ctypes
import logging
import os
import platform
import subprocess
import sys


LOGGER = logging.getLogger("nvidia_libraries")

# The sonames every CUDA job resolves through the jail's ld.so.cache. Loading them exercises
# exactly the path a job takes, so a 0-byte target or a dangling symlink fails here first.
REQUIRED_SONAMES = ("libcuda.so.1", "libnvidia-ml.so.1")

# The jail flattens every driver library into this directory (see jail_mount_target in complement_jail.sh).
LIB_DIR = f"/usr/lib/{platform.machine()}-linux-gnu"

# Same threshold as lib_has_real_content in complement_jail.sh: placeholders are small marker
# files, no real driver library is this small.
MIN_REAL_LIBRARY_SIZE = 1024

NVIDIA_CONTAINER_CLI_LIST = ["nvidia-container-cli", "list", "--libraries"]

# WARNING: this duplicates the private compute_libs and utility_libs tables from libnvidia-container
# (src/nvc_info.c, checked against v1.18.2). `nvidia-container-cli list` cannot filter by capability,
# while complement_jail.sh mounts only the capabilities from NVIDIA_DRIVER_CAPABILITIES (compute and
# utility are always among them) and leaves small placeholder files for the rest. So only the libraries
# of these two capabilities are guaranteed to be real files on every GPU worker. Review this table when
# upgrading nvidia-container-toolkit in the jail image; a library missing here is merely not checked,
# a library wrongly added here drains healthy nodes. The PKCS#11 module (libnvidia-pkcs11*.so) is left
# out on purpose: its two builds need OpenSSL 1.1 and OpenSSL 3 respectively, so one of them never
# loads regardless of the driver mounts.
CHECKED_LIBRARY_PREFIXES = (
    # utility
    "libnvidia-ml.so",
    "libnvidia-cfg.so",
    "libnvidia-nscq.so",
    # compute
    "libcuda.so",
    "libcudadebugger.so",
    "libnvidia-opencl.so",
    "libnvidia-gpucomp.so",
    "libnvidia-ptxjitcompiler.so",
    "libnvidia-fatbinaryloader.so",
    "libnvidia-allocator.so",
    "libnvidia-compiler.so",
    "libnvidia-nvvm.so",
)


def write_details(message: str) -> None:
    os.write(3, f"{message}\n".encode("utf-8", errors="backslashreplace"))


# Details become part of the Slurm drain reason, so they are one of three fixed phrases;
# the exact size and the dlopen error text go to the check log only. The file checks trust complement_jail.sh to put
# every driver library and its soname into LIB_DIR, so `path` is where dlopen of `name` ends up.
def load_library_failure(name: str, path: str) -> str | None:
    try:
        size = os.stat(path).st_size
    except FileNotFoundError:
        LOGGER.warning("%s is missing", path)
        return "missing"
    if size <= MIN_REAL_LIBRARY_SIZE:
        LOGGER.warning("%s is %d bytes, expected a real library", path, size)
        return "file too short"
    try:
        # Lazy binding keeps the load cheap; NEEDED dependencies are still resolved, so a corrupt ELF
        # or a missing dependency fails here
        ctypes.CDLL(name, mode=ctypes.RTLD_LOCAL | os.RTLD_LAZY)
    except OSError as e:
        LOGGER.warning("Load %s: %s", name, e)
        return "not loadable"
    LOGGER.info("Loaded %s", name)
    return None


def is_checked_library(name: str) -> bool:
    return any(name.startswith(prefix + ".") for prefix in CHECKED_LIBRARY_PREFIXES)


def list_driver_libraries() -> list[str]:
    try:
        result = subprocess.run(
            NVIDIA_CONTAINER_CLI_LIST, check=False, stdout=subprocess.PIPE, stderr=subprocess.STDOUT, text=True
        )
    except OSError as e:
        LOGGER.warning("Run %s: %s; skipping the driver library file check", NVIDIA_CONTAINER_CLI_LIST[0], e)
        return []
    if result.returncode != 0:
        LOGGER.warning(
            "%s exited with %d, skipping the driver library file check:\n%s",
            " ".join(NVIDIA_CONTAINER_CLI_LIST),
            result.returncode,
            result.stdout.rstrip("\n"),
        )
        return []
    names = [os.path.basename(line.strip()) for line in result.stdout.splitlines() if line.strip()]
    checked = [name for name in names if is_checked_library(name)]
    LOGGER.info("nvidia-container-cli listed %d libraries, %d belong to the compute and utility capabilities", len(names), len(checked))
    return checked


def main() -> int:
    logging.basicConfig(level=logging.INFO, format="%(message)s")
    try:
        if os.environ.get("NODESET_GPU_ENABLED") != "true":
            LOGGER.info("Not a GPU worker, skipping")
            return 0

        # Sonames are loaded by name so the jail's ld.so.cache resolves them, exactly as a job would;
        # the driver files from nvidia-container-cli are loaded by their path in the jail.
        libraries = [(soname, os.path.join(LIB_DIR, soname)) for soname in REQUIRED_SONAMES]
        libraries += [(os.path.join(LIB_DIR, name), os.path.join(LIB_DIR, name)) for name in list_driver_libraries()]
        for name, path in libraries:
            failure = load_library_failure(name, path)
            if failure is not None:
                write_details(f"{os.path.basename(path)}: {failure}")
                return 1

        LOGGER.info("All %d NVIDIA libraries are loadable", len(libraries))
        return 0
    except Exception:
        LOGGER.exception("Unhandled exception, skipping the check")
        return 0


if __name__ == "__main__":
    sys.exit(main())
