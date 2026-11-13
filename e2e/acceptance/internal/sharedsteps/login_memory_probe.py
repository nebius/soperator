import argparse
import grp
import hashlib
import json
import os
import pathlib
import subprocess
import sys
import time


def parse_arguments(argv):
    parser = argparse.ArgumentParser()
    parser.add_argument("--memory-high", type=int, required=True)
    parser.add_argument("--memory-max", type=int, required=True)
    parser.add_argument("--held-bytes", type=int, required=True)
    parser.add_argument("--hold-seconds", type=float, required=True)
    parser.add_argument("--workload-timeout", type=float, required=True)
    parser.add_argument("action", choices=("environment", "inspect", "services", "hold", "survivor", "allocate", "high", "hard", "smoke"))
    parser.add_argument("arguments", nargs="*")
    config = parser.parse_args(argv)
    if not 0 < config.held_bytes < config.memory_high < config.memory_max or config.workload_timeout <= 0 or config.hold_seconds <= 0:
        parser.error("invalid memory limits, held size or workload timeout")
    return config


def config_arguments(config):
    return [
        "--memory-high", str(config.memory_high), "--memory-max", str(config.memory_max),
        "--held-bytes", str(config.held_bytes), "--workload-timeout", str(config.workload_timeout),
        "--hold-seconds", str(config.hold_seconds),
    ]


def process_cgroup(pid="self"):
    relative = next(
        line[3:] for line in pathlib.Path(f"/proc/{pid}/cgroup").read_text().splitlines()
        if line.startswith("0::")
    )
    return pathlib.Path("/sys/fs/cgroup") / relative.lstrip("/")


def environment(group, root=pathlib.Path("/sys/fs/cgroup"), swaps=pathlib.Path("/proc/swaps")):
    limits = {}
    for ancestor in (group, *group.parents):
        if ancestor != root and root not in ancestor.parents:
            break
        try:
            value = (ancestor / "memory.swap.max").read_text().strip()
        except FileNotFoundError:
            continue
        assert value == "max" or int(value) >= 0, (str(ancestor), value)
        limits[str(ancestor)] = value
    return {
        "swap": swaps.exists() and len(swaps.read_text().splitlines()) > 1,
        "swap_disabled": "0" in limits.values(), "swap_limits": limits,
    }


def user_cgroup():
    group = process_cgroup()
    while group.name != f"user-{os.getuid()}":
        assert group != pathlib.Path("/sys/fs/cgroup"), "SSH session outside the user cgroup"
        group = group.parent
    assert os.getuid() >= 1000 and group.parent.name == "users", str(group)
    return group


def counters(group, filename="memory.events"):
    return {key: int(value) for key, value in (
        line.split() for line in (group / filename).read_text().splitlines()
    )}


def snapshot(group):
    def optional(filename):
        try:
            return (group / filename).read_text().strip()
        except FileNotFoundError:
            return None

    peak = optional("memory.peak")
    return {
        "cgroup": str(group),
        "current": int((group / "memory.current").read_text()),
        "high": (group / "memory.high").read_text().strip(),
        "max": (group / "memory.max").read_text().strip(),
        "swap_max": optional("memory.swap.max"),
        "events": counters(group),
        "pressure": optional("memory.pressure"),
        "peak": int(peak) if peak is not None else None,
    }


def emit(value):
    print("LOGIN_MEMORY=" + json.dumps(value), flush=True)


def publish(filename, value):
    filename = pathlib.Path(filename)
    temporary = filename.with_suffix(".tmp")
    temporary.write_text(json.dumps(value))
    temporary.replace(filename)


def hold(directory, held_bytes):
    group = user_cgroup()
    block = bytearray(b"b") * held_bytes
    initial = hashlib.sha256(block).hexdigest()
    ready = {"pid": os.getpid(), "uid": os.getuid(), "cgroup": str(group), "checksum": initial}
    publish(directory / "ready", ready)
    deadline = time.monotonic() + 1200
    while time.monotonic() < deadline:
        if (directory / "challenge").exists():
            ready["checksum"] = hashlib.sha256(block).hexdigest()
            publish(directory / "verified", ready)
            (directory / "challenge").unlink()
        time.sleep(0.2)


def allocate(phase, config):
    group = user_cgroup()
    high, maximum = int((group / "memory.high").read_text()), int((group / "memory.max").read_text())
    assert (high, maximum) == (config.memory_high, config.memory_max), "unexpected per-user limits"
    initial_high = counters(group)["high"]
    blocks = []
    sampled_peak = 0
    for _ in range((config.memory_max + 64 * 1024 * 1024) // (1024 * 1024)):
        blocks.append(bytearray(b"a") * (1024 * 1024))
        current = int((group / "memory.current").read_text())
        sampled_peak = max(sampled_peak, current)
        if phase == "high" and current > high and counters(group)["high"] > initial_high:
            started = time.monotonic()
            samples = [{"current": current, "high_events": counters(group)["high"], "elapsed_seconds": 0}]
            while True:
                remaining = config.hold_seconds - (time.monotonic() - started)
                if remaining > 0:
                    time.sleep(min(0.1, remaining))
                current = int((group / "memory.current").read_text())
                sampled_peak = max(sampled_peak, current)
                samples.append({"current": current, "high_events": counters(group)["high"], "elapsed_seconds": time.monotonic() - started})
                if samples[-1]["elapsed_seconds"] >= config.hold_seconds:
                    break
            emit({"held": snapshot(group), "sampled_peak": sampled_peak, "held_seconds": time.monotonic() - started, "samples": samples})
            return
    emit({"limit_bypassed": True, "sampled_peak": sampled_peak})


def workload(phase, config):
    group = user_cgroup()
    before = snapshot(group)
    assert (before["high"], before["max"]) == (str(config.memory_high), str(config.memory_max)), "unexpected per-user limits"
    child = subprocess.Popen(
        [sys.executable, __file__, *config_arguments(config), "allocate", phase],
        stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True,
    )
    timed_out = False
    deadline = time.monotonic() + config.workload_timeout
    sampled_peak = 0
    while True:
        sampled_peak = max(sampled_peak, int((group / "memory.current").read_text()))
        remaining = deadline - time.monotonic()
        if remaining <= 0:
            timed_out = True
            child.kill()
            output, errors = child.communicate()
            break
        try:
            output, errors = child.communicate(timeout=min(0.05, remaining))
            break
        except subprocess.TimeoutExpired:
            pass
    payload = {}
    for line in output.splitlines():
        if line.startswith("LOGIN_MEMORY="):
            payload = json.loads(line.split("=", 1)[1])
    emit({
        "before": before, "after": snapshot(group), "returncode": child.returncode,
        "timed_out": timed_out, "allocation": payload, "stderr": errors,
        "sampled_peak": sampled_peak,
    })


def services(group, target):
    emit({
        "container_local": counters(group, "memory.events.local"),
        "container": counters(group), "target": counters(target),
        "init": counters(group / "init"),
    })


def survivor(directory):
    ready = json.loads((directory / "ready").read_text())
    pid = ready["pid"]
    os.kill(pid, 0)
    status = dict(line.split(":", 1) for line in pathlib.Path(f"/proc/{pid}/status").read_text().splitlines())
    assert status["State"].strip()[0] != "Z", status["State"]
    assert int(status["Uid"].split()[0]) == ready["uid"], status["Uid"]
    process_group = process_cgroup(pid)
    group = pathlib.Path(ready["cgroup"])
    assert process_group == group or group in process_group.parents, str(process_group)
    if not (directory / "verified").exists():
        (directory / "challenge").touch()
        return {"ready": False}
    verified = json.loads((directory / "verified").read_text())
    assert verified == ready, (ready, verified)
    return {"ready": True, "process": verified, "memory": snapshot(group)}


if __name__ == "__main__":
    config = parse_arguments(sys.argv[1:])
    action = config.action
    if action == "environment":
        emit(environment(process_cgroup()))
    elif action == "inspect":
        group = user_cgroup()
        emit({
            "uid": os.getuid(), "hostname": os.uname().nodename, "kernel": os.uname().release,
            "groups": [grp.getgrgid(gid).gr_name for gid in os.getgroups()],
            "session": str(process_cgroup()), "memory": snapshot(group),
            **environment(group),
        })
    elif action == "services":
        services(pathlib.Path(config.arguments[0]), pathlib.Path(config.arguments[1]))
    elif action == "hold":
        hold(pathlib.Path(config.arguments[0]), config.held_bytes)
    elif action == "survivor":
        try:
            emit(survivor(pathlib.Path(config.arguments[0])))
        except (AssertionError, OSError, ValueError, KeyError) as error:
            emit({"error": f"{type(error).__name__}: {error}"})
    elif action == "allocate":
        allocate(config.arguments[0], config)
    elif action in ("high", "hard"):
        workload(action, config)
    elif action == "smoke":
        block = bytearray(b"c") * config.held_bytes
        output = subprocess.check_output(["sinfo", "-h", "-o", "%P"], text=True)
        assert output.strip(), "empty Slurm partition list"
        emit({"allocated": len(block), "partitions": output})
    else:
        raise ValueError(action)
