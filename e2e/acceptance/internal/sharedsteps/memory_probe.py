import hashlib
import json
import os
import pathlib
import sys
import time


MIB = 1024 * 1024


def publish(filename, value):
    temporary = filename.with_suffix(".tmp")
    temporary.write_text(json.dumps(value))
    temporary.replace(filename)


def identity():
    stat = pathlib.Path("/proc/self/stat").read_text().rsplit(")", 1)[1].split()
    relative = next(line[3:] for line in pathlib.Path("/proc/self/cgroup").read_text().splitlines() if line.startswith("0::"))
    return {"pid": os.getpid(), "start_ticks": stat[19], "task_cgroup": relative, "python_version": sys.version}


def wait_for(filename, timeout=300):
    deadline = time.monotonic() + timeout
    while not filename.exists():
        if time.monotonic() >= deadline:
            raise TimeoutError(str(filename))
        time.sleep(0.05)


def identify(directory, role):
    relative = next(line[3:] for line in pathlib.Path("/proc/self/cgroup").read_text().splitlines() if line.startswith("0::"))
    publish(directory / (role + ".batch.json"), {
        "job_id": os.environ["SLURM_JOB_ID"], "task_cgroup": relative,
        "hostname": os.uname().nodename, "kernel": os.uname().release,
    })


def target(directory, limit):
    process = identity()
    publish(directory / "target.ready.json", process)
    wait_for(directory / "target.start")
    blocks = [bytearray(b"a") * MIB for _ in range(limit // (4 * MIB))]
    started = time.monotonic()
    time.sleep(3)
    publish(directory / "target.below.json", {
        **process, "allocated": len(blocks) * MIB, "held_seconds": time.monotonic() - started,
    })
    wait_for(directory / "target.exceed")
    # Stop at a finite ceiling even if enforcement is broken.
    while len(blocks) * MIB < limit + limit // 4:
        blocks.append(bytearray(b"a") * MIB)
    publish(directory / "target.bypassed.json", {**process, "allocated": len(blocks) * MIB})
    return 42


def neighbor(directory, held):
    block = bytearray(b"b") * held
    checksum = hashlib.sha256(block).hexdigest()
    process = {**identity(), "allocated": held, "checksum": checksum}
    publish(directory / "neighbor.ready.json", process)
    deadline = time.monotonic() + 900
    challenge = ""
    while time.monotonic() < deadline:
        request = directory / "neighbor.challenge"
        if request.exists() and request.read_text() != challenge:
            challenge = request.read_text()
            current = hashlib.sha256(block).hexdigest()
            if current != checksum:
                raise AssertionError("neighboring job's memory changed")
            publish(directory / "neighbor.verified.json", {**process, "challenge": challenge})
        time.sleep(0.1)
    raise TimeoutError("neighboring workload expired")


def counters(filename):
    return dict((key, int(value)) for key, value in (line.split() for line in filename.read_text().splitlines()))


def locate(directory, role, start_ticks, proc=pathlib.Path("/proc")):
    expected = [str(directory / "probe.py"), role, str(directory)]
    matches = []
    for entry in proc.glob("[0-9]*"):
        try:
            arguments = (entry / "cmdline").read_bytes().decode(errors="replace").split("\0")
            if not any(arguments[index:index + 3] == expected for index in range(len(arguments) - 2)):
                continue
            started = (entry / "stat").read_text().rsplit(")", 1)[1].split()[19]
            if started != start_ticks:
                continue
            relative = next(line[3:] for line in (entry / "cgroup").read_text().splitlines() if line.startswith("0::"))
            matches.append({"pid": int(entry.name), "start_ticks": started, "task_cgroup": relative})
        except (FileNotFoundError, ProcessLookupError):
            pass
    if len(matches) != 1:
        raise AssertionError(f"expected one {role} process with start time {start_ticks}, found {matches}")
    return matches[0]


def snapshot(group, root=pathlib.Path("/sys/fs/cgroup"), swaps=pathlib.Path("/proc/swaps")):
    maximum = (group / "memory.max").read_text().strip()
    events = counters(group / "memory.events")
    swap_limits = {}
    for ancestor in (group, *group.parents):
        if ancestor != root and root not in ancestor.parents:
            break
        swap = ancestor / "memory.swap.max"
        if swap.exists():
            swap_limits[str(ancestor)] = swap.read_text().strip()
    return {
        "cgroup": str(group), "maximum": maximum,
        "current": int((group / "memory.current").read_text()), "events": events,
        "swap": len(swaps.read_text().splitlines()) > 1, "swap_limits": swap_limits,
    }


def observe(directory, group, timeout):
    result = {"finished": False, "timed_out": False, "removed": False}
    try:
        before = snapshot(group)
        result.update(before=before, after={**before, "events": dict(before["events"])}, sampled_peak=before["current"])
        publish(directory / "observer.json", result)
        deadline = time.monotonic() + timeout
        # Keep the event file open while Slurm removes the step/job hierarchy.
        with (group / "memory.events").open() as events:
            while not (directory / "observer.stop").exists():
                events.seek(0)
                try:
                    latest = dict((key, int(value)) for key, value in (line.split() for line in events.read().splitlines()))
                    result["after"]["events"] = latest
                    result["after"]["current"] = int((group / "memory.current").read_text())
                    result["sampled_peak"] = max(result["sampled_peak"], result["after"]["current"])
                except FileNotFoundError:
                    result["removed"] = True
                    break
                if latest["oom_kill"] > before["events"]["oom_kill"]:
                    break
                if time.monotonic() >= deadline:
                    result["timed_out"] = True
                    break
                time.sleep(0.02)
    except Exception as error:
        result["error"] = f"{type(error).__name__}: {error}"
    finally:
        result["finished"] = True
        publish(directory / "observer.json", result)


def main(arguments):
    action, location, *extra = arguments
    directory = pathlib.Path(location)
    if action == "identify":
        identify(directory, extra[0])
    elif action == "target":
        return target(directory, int(extra[0]))
    elif action == "neighbor":
        neighbor(directory, int(extra[0]))
    elif action == "snapshot":
        print(json.dumps(snapshot(directory)))
    elif action == "locate":
        print(json.dumps(locate(directory, extra[0], extra[1])))
    elif action == "observe":
        observe(directory, pathlib.Path(extra[0]), float(extra[1]))
    else:
        raise ValueError(action)
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
