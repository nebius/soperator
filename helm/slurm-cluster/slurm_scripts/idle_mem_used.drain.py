import json
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


def run_capture(command: list[str]) -> subprocess.CompletedProcess[str]:
    try:
        return subprocess.run(
            command,
            check=False,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            text=True,
        )
    except OSError as e:
        return subprocess.CompletedProcess(
            args=command,
            returncode=127,
            stdout=f"{command[0]}: {e}\n",
        )


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


def get_local_job_count(listjobs_output: str) -> int | None:
    try:
        data = json.loads(listjobs_output)
    except json.JSONDecodeError:
        return None

    jobs = data.get("jobs")
    if not isinstance(jobs, list):
        return None

    return len(jobs)


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


def write_drain_reason(message: str) -> None:
    os.write(3, f"{message}\n".encode("utf-8", errors="backslashreplace"))


def main() -> int:
    configure_logging()

    node_name = os.environ.get("SLURMD_NODENAME", "unknown")
    node_real_memory_raw = os.environ.get("CHECKS_NODE_REAL_MEM_BYTES", "")

    LOGGER.info("[%s] Check memory usage when node %s is idle", time.ctime(), node_name)
    LOGGER.info(
        "Slurm RealMemory input: "
        "%s bytes",
        node_real_memory_raw if node_real_memory_raw else "<unavailable>",
    )

    listjobs_result = run_capture(["scontrol", "listjobs", "--json"])
    listjobs_output = listjobs_result.stdout.rstrip("\n")

    LOGGER.info("scontrol listjobs --json exit code: %s", listjobs_result.returncode)
    LOGGER.info(
        "scontrol listjobs --json output: "
        "%s",
        listjobs_output if listjobs_output else "<empty>",
    )

    if listjobs_result.returncode != 0:
        LOGGER.warning(
            "Could not determine whether the node is idle because "
            "'scontrol listjobs --json' failed; skipping memory validation"
        )
        return 0

    local_job_count = get_local_job_count(listjobs_output)
    if local_job_count is None:
        LOGGER.warning(
            "Could not determine whether the node is idle because "
            "'scontrol listjobs --json' returned invalid job data; "
            "skipping memory validation"
        )
        return 0

    LOGGER.info("Local Slurm job count from JSON .jobs array: %s", local_job_count)
    node_is_idle = local_job_count == 0
    if node_is_idle:
        LOGGER.info("The JSON .jobs array is empty; treating the node as idle")
    else:
        LOGGER.info(
            "The JSON .jobs array contains local jobs; treating the node as non-idle"
        )

    LOGGER.info("Node is idle: %s", str(node_is_idle).lower())
    if not node_is_idle:
        LOGGER.info("Node has local jobs; skipping memory validation")
        return 0

    if not node_real_memory_raw.isdecimal() or int(node_real_memory_raw) <= 0:
        real_memory_display = node_real_memory_raw or "<unavailable>"
        LOGGER.warning(
            "Invalid or unavailable Slurm RealMemory '%s'; expected a positive "
            "byte count, skipping memory validation",
            real_memory_display,
        )
        return 0

    node_real_memory_bytes = int(node_real_memory_raw)
    free_bytes_result = run_capture_c_locale(["free", "-b"])
    if free_bytes_result.returncode != 0:
        LOGGER.warning(
            "Could not read local memory information with 'free -b'; "
            "skipping memory validation"
        )
        return 0

    memory_values = parse_memory_values(free_bytes_result.stdout)
    if memory_values is None:
        LOGGER.warning(
            "Could not determine valid total and available memory from 'free -b'; "
            "skipping memory validation"
        )
        return 0

    mem_total_bytes, mem_available_bytes = memory_values

    if node_real_memory_bytes > mem_total_bytes:
        LOGGER.warning(
            "Slurm RealMemory %s bytes exceeds MemTotal %s bytes; "
            "skipping memory validation",
            node_real_memory_bytes,
            mem_total_bytes,
        )
        return 0

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
        write_drain_reason(
            f"available memory {mem_available_gb} GB < configured "
            f"{node_real_memory_gb} GB; stop leftover processes or reboot"
        )
        return 1

    LOGGER.info("Idle node leaves enough memory available for Slurm RealMemory")
    return 0


if __name__ == "__main__":
    sys.exit(main())
