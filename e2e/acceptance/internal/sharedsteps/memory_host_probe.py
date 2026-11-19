import errno
import json
import os
import pathlib
import re
import sys


def inspect_host(since):
    processes = {}
    for entry in pathlib.Path("/proc").glob("[0-9]*"):
        try:
            command = pathlib.Path((entry / "cmdline").read_bytes().split(b"\0")[0].decode()).name
            if command in ("kubelet", "nvidia-hostengine", "slurmd", "munged", "dockerd"):
                start = (entry / "stat").read_text().rsplit(")", 1)[1].split()[19]
                processes[entry.name] = {"command": command, "start_ticks": start}
        except (FileNotFoundError, ProcessLookupError):
            pass
    first, last, messages = None, -1, []
    descriptor = os.open("/dev/kmsg", os.O_RDONLY | os.O_NONBLOCK)
    try:
        while True:
            try:
                record = os.read(descriptor, 1024 * 1024).decode(errors="replace")
            except OSError as error:
                if error.errno == errno.EAGAIN:
                    break
                raise
            header, message = record.split(";", 1)
            sequence = int(header.split(",")[1])
            first = sequence if first is None else first
            last = sequence
            if sequence > since and re.search(r"oom-kill:|[Oo]ut of memory:|[Kk]illed process|oom_reaper:", message):
                messages.append({"sequence": sequence, "message": message.strip()})
    finally:
        os.close(descriptor)
    available = next(int(line.split()[1]) * 1024 for line in pathlib.Path("/proc/meminfo").read_text().splitlines() if line.startswith("MemAvailable:"))
    return {
        "boot_id": pathlib.Path("/proc/sys/kernel/random/boot_id").read_text().strip(),
        "kernel": os.uname().release, "processes": processes, "available": available,
        "first_sequence": first, "last_sequence": last, "oom_messages": messages,
    }


if __name__ == "__main__":
    print("MEMORY_HOST=" + json.dumps(inspect_host(int(sys.argv[1]))))
