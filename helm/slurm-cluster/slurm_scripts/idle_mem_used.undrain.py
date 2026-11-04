import logging
import os
import subprocess
import sys
import time


BYTES_PER_GB = 1_000_000_000
LOGGER = logging.getLogger("idle_mem_used")


class InfoFilter(logging.Filter):
    def filter(self, record: logging.LogRecord) -> bool:
        return record.levelno < logging.WARNING


def configure_logging() -> None:
    stdout_handler = logging.StreamHandler(sys.stdout)
    stdout_handler.setLevel(logging.DEBUG)
    stdout_handler.addFilter(InfoFilter())
    stdout_handler.setFormatter(logging.Formatter("%(message)s"))

    stderr_handler = logging.StreamHandler(sys.stderr)
    stderr_handler.setLevel(logging.WARNING)
    stderr_handler.setFormatter(logging.Formatter("%(message)s"))

    LOGGER.setLevel(logging.INFO)
    LOGGER.handlers.clear()
    LOGGER.addHandler(stdout_handler)
    LOGGER.addHandler(stderr_handler)
    LOGGER.propagate = False


def run_capture_c_locale(command: list[str]) -> subprocess.CompletedProcess[str]:
    env = os.environ.copy()
    env["LC_ALL"] = "C"
    try:
        return subprocess.run(
            command,
            check=False,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
            env=env,
        )
    except OSError as e:
        return subprocess.CompletedProcess(
            args=command,
            returncode=127,
            stdout=f"{command[0]}: {e}\n",
        )


def parse_memory_values(free_bytes_output: str) -> tuple[int, int] | None:
    for line in free_bytes_output.splitlines():
        fields = line.split()
        if not fields or fields[0] != "Mem:":
            continue

        if len(fields) < 7:
            return None

        if not fields[1].isdecimal() or not fields[6].isdecimal():
            return None

        mem_total_bytes = int(fields[1])
        mem_available_bytes = int(fields[6])
        if mem_available_bytes > mem_total_bytes:
            return None

        return mem_total_bytes, mem_available_bytes

    return None


def bytes_to_gb(value: int) -> str:
    return f"{value / BYTES_PER_GB:.2f}"


def log_human_memory_snapshot() -> None:
    LOGGER.info("Memory snapshot (free -hw):")
    result = run_capture_c_locale(["free", "-hw"])
    if result.stdout:
        LOGGER.info(result.stdout.rstrip("\n"))

    if result.returncode != 0:
        LOGGER.warning(
            "Could not print the human-readable memory snapshot with 'free -hw'"
        )


def main() -> int:
    configure_logging()

    node_name = os.environ.get("SLURMD_NODENAME", "unknown")
    node_real_memory_raw = os.environ.get("CHECKS_NODE_REAL_MEM_BYTES", "")

    LOGGER.info(
        "[%s] Check whether memory usage has recovered on drained node %s",
        time.ctime(),
        node_name,
    )
    LOGGER.info(
        "Slurm RealMemory input: %s bytes",
        node_real_memory_raw if node_real_memory_raw else "<unavailable>",
    )
    LOGGER.info(
        "Node eligibility source: this check is scheduled only for drained nodes"
    )

    if not node_real_memory_raw.isdecimal() or int(node_real_memory_raw) <= 0:
        real_memory_display = node_real_memory_raw or "<unavailable>"
        LOGGER.warning(
            "Invalid or unavailable Slurm RealMemory '%s'; keeping node drained",
            real_memory_display,
        )
        return 1

    node_real_memory_bytes = int(node_real_memory_raw)
    free_bytes_result = run_capture_c_locale(["free", "-b"])
    if free_bytes_result.returncode != 0:
        LOGGER.warning(
            "Could not read local memory information with 'free -b'; "
            "keeping node drained"
        )
        return 1

    memory_values = parse_memory_values(free_bytes_result.stdout)
    if memory_values is None:
        LOGGER.warning(
            "Could not determine valid total and available memory from 'free -b'; "
            "keeping node drained"
        )
        return 1

    mem_total_bytes, mem_available_bytes = memory_values
    if node_real_memory_bytes > mem_total_bytes:
        LOGGER.warning(
            "Slurm RealMemory %s bytes exceeds MemTotal %s bytes; "
            "keeping node drained",
            node_real_memory_bytes,
            mem_total_bytes,
        )
        return 1

    mem_available_gb = bytes_to_gb(mem_available_bytes)
    node_real_memory_gb = bytes_to_gb(node_real_memory_bytes)

    LOGGER.info(
        "Memory comparison: "
        "available=%s GB (%s bytes), "
        "Slurm RealMemory=%s GB (%s bytes)",
        mem_available_gb,
        mem_available_bytes,
        node_real_memory_gb,
        node_real_memory_bytes,
    )
    log_human_memory_snapshot()

    if mem_available_bytes < node_real_memory_bytes:
        LOGGER.warning(
            "Available memory %s GB is still below configured memory %s GB; "
            "keeping node drained",
            mem_available_gb,
            node_real_memory_gb,
        )
        return 1

    LOGGER.info(
        "Drained node leaves enough memory available for Slurm RealMemory; "
        "memory recovery confirmed"
    )
    return 0


if __name__ == "__main__":
    sys.exit(main())
