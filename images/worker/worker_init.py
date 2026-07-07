#!/usr/bin/env python3
"""
Worker initialization tasks for Slurm.

Supports two modes:
  wait-controller  - Wait for Slurm controller (slurmctld) to be ready
  wait-topology    - Wait for topology data from ConfigMap (for ephemeral nodes)

Environment Variables (wait-controller):
    CONTROLLER_MAX_ATTEMPTS: Max ping attempts (default: 60)
    CONTROLLER_POLL_INTERVAL: Seconds between attempts (default: 5)

Environment Variables (wait-topology):
    K8S_NODE_NAME: Kubernetes node name (required, from Downward API)
    TOPOLOGY_CONFIGMAP_PATH: Path to mounted ConfigMap (default: /tmp/slurm/topology-node-labels)
    TOPOLOGY_WAIT_TIMEOUT: Max wait time in seconds (default: 180)
    TOPOLOGY_POLL_INTERVAL: Poll interval in seconds (default: 5)
    SLURM_TOPOLOGY_PLUGIN: Slurm topology plugin override (default: read from slurm.conf)
"""

import argparse
import json
import logging
import os
import re
import shutil
import subprocess
import sys
import time
from pathlib import Path
from typing import Any

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s - %(levelname)s - %(message)s",
    datefmt="%Y-%m-%d %H:%M:%S",
)
logger: logging.Logger = logging.getLogger(__name__)

# Constants
SLURM_CONFIG_LINK_SOURCE: Path = Path("/mnt/jail/etc/slurm")
SLURM_CONFIG_LINK_TARGET: Path = Path("/etc/slurm")
SLURM_CONFIG_PATH: Path = Path("/etc/slurm/slurm.conf")
SLURM_TOPOLOGY_CONFIG_PATH: Path = Path("/etc/slurm/topology.conf")

TOPOLOGY_PLUGIN_TREE: str = "topology/tree"
TOPOLOGY_PLUGIN_BLOCK: str = "topology/block"


# region Common env functions


def get_from_env_required(name: str) -> str:
    """Get a value from environment variable."""
    value: str | None = os.environ.get(name)

    if value is None:
        logger.error("Environment variable %s must be set", name)
        sys.exit(1)

    return str(value)


# endregion Common env functions

# region Controller readiness functions


def create_slurm_config_symlink() -> None:
    """Create symlink to Slurm configs from jail mount."""
    source: Path = Path(SLURM_CONFIG_LINK_SOURCE)
    target: Path = Path(SLURM_CONFIG_LINK_TARGET)

    logger.info(
        "Creating symlink to slurm configs: %s -> %s",
        target,
        source,
    )

    if target.is_symlink() or target.is_file():
        target.unlink()
    elif target.is_dir():
        shutil.rmtree(target)

    target.symlink_to(source)


def get_controller_max_attempts() -> int:
    """Get the maximum number of controller ping attempts."""
    return int(os.environ.get("CONTROLLER_MAX_ATTEMPTS", "60"))


def get_controller_poll_interval() -> int:
    """Get the poll interval in seconds for controller readiness checks."""
    return int(os.environ.get("CONTROLLER_POLL_INTERVAL", "5"))


def get_node_addr() -> str:
    pod_name: str = get_from_env_required("K8S_POD_NAME")
    logger.info("Using K8S_POD_NAME=%s for NodeAddr construction", pod_name)

    service_name: str = get_from_env_required("K8S_SERVICE_NAME")
    logger.info("Using K8S_SERVICE_NAME=%s for NodeAddr construction", service_name)

    pod_namespace: str = get_from_env_required("K8S_POD_NAMESPACE")
    logger.info("Using K8S_POD_NAMESPACE=%s for NodeAddr construction", pod_namespace)

    return f"nodeaddr={pod_name}.{service_name}.{pod_namespace}.svc"


def wait_for_controller() -> None:
    """Wait for slurmctld to be ready by polling scontrol ping."""
    max_attempts: int = get_controller_max_attempts()
    poll_interval: int = get_controller_poll_interval()

    logger.info("Waiting for Slurm controller to be ready...")
    logger.info("Max attempts: %d, Poll interval: %ds", max_attempts, poll_interval)

    create_slurm_config_symlink()

    logger.info("Checking slurmctld readiness...")
    for attempt in range(max_attempts):
        logger.info(
            "Attempt %d/%d: Checking controller readiness...",
            attempt + 1,
            max_attempts,
        )

        try:
            result: subprocess.CompletedProcess[str] = subprocess.run(
                ["scontrol", "ping", "--json"],
                capture_output=True,
                text=True,
                timeout=30,
            )
            if result.returncode == 0:
                try:
                    data: Any = json.loads(result.stdout)
                    pings: list[dict[str, Any]] = data.get("pings", [])
                    if pings and all(
                        p.get("responding") is True and p.get("pinged") == "UP"
                        for p in pings
                    ):
                        logger.info("Controller is ready!")
                        logger.info(
                            "scontrol ping output:\n%s",
                            json.dumps(pings, indent=2),
                        )
                        return
                    else:
                        logger.warning(
                            "Controller not ready yet: %s",
                            json.dumps(pings, indent=2),
                        )
                except json.JSONDecodeError:
                    logger.warning(
                        "Failed to parse scontrol ping JSON output:\n%s",
                        result.stdout.strip(),
                    )
            else:
                output: str = (result.stdout + result.stderr).strip()
                logger.warning("scontrol ping failed with output:\n%s", output)
        except subprocess.TimeoutExpired:
            logger.warning("scontrol ping timed out")
        except FileNotFoundError:
            logger.error("scontrol command not found")
            sys.exit(1)

        time.sleep(poll_interval)

    logger.error("Controller did not become ready after %d attempts", max_attempts)
    sys.exit(1)


# endregion Controller readiness functions

# region Topology functions


def get_node_name() -> str:
    """Get the Kubernetes node name from environment variable."""
    return os.environ["K8S_NODE_NAME"]


def get_topology_path() -> Path:
    """Get the path to the topology ConfigMap mount."""
    return Path(
        os.environ.get("TOPOLOGY_CONFIGMAP_PATH", "/tmp/slurm/topology-node-labels")
    )


def get_topology_wait_timeout() -> int:
    """Get the maximum wait timeout in seconds."""
    return int(os.environ.get("TOPOLOGY_WAIT_TIMEOUT", "180"))


def get_topology_poll_interval() -> int:
    """Get the poll interval in seconds."""
    return int(os.environ.get("TOPOLOGY_POLL_INTERVAL", "5"))


def get_topology_plugin(slurm_conf_path: Path = SLURM_CONFIG_PATH) -> str:
    """Get the configured Slurm topology plugin."""
    topology_plugin: str = os.environ.get("SLURM_TOPOLOGY_PLUGIN", "").strip()
    if topology_plugin:
        return topology_plugin.lower()

    slurm_conf_path: Path = Path(slurm_conf_path)
    try:
        with slurm_conf_path.open("r") as f:
            pattern: re.Pattern[str] = re.compile(
                r"^TopologyPlugin\s*=\s*(\S+)", re.IGNORECASE
            )
            for line in f:
                line: str = line.split("#", 1)[0].strip()
                match: re.Match[str] | None = re.match(pattern, line)
                if match:
                    return match.group(1).lower()
    except (IOError, OSError) as e:
        logger.info("Failed to read topology plugin from %s: %s", slurm_conf_path, e)

    return TOPOLOGY_PLUGIN_TREE


def topology_conf_contains_hostname(topology_conf_path: Path, hostname: str) -> bool:
    """Check whether topology.conf contains the given hostname in a nodes list."""
    topology_conf_path: Path = Path(topology_conf_path)
    if not topology_conf_path.is_file():
        return False

    try:
        content: str = topology_conf_path.read_text()
    except (IOError, OSError) as e:
        logger.warning("Failed to read topology config %s: %s", topology_conf_path, e)
        return False

    return any(
        nodes == "ALL" or _slurm_hostlist_contains(nodes, hostname)
        for nodes in _topology_nodes_values(content)
    )


def _topology_nodes_values(content: str) -> list[str]:
    """Extract Nodes= values from topology.conf content."""
    values: list[str] = []
    pattern: re.Pattern[str] = re.compile(r"(?:^|\s)Nodes=(\S+)")

    for raw_line in content.splitlines():
        line: str = raw_line.split("#", 1)[0].strip()
        match: re.Match[str] | None = pattern.search(line)
        if match:
            values.append(match.group(1))

    return values


def _slurm_hostlist_contains(hostlist: str, hostname: str) -> bool:
    """Return True when a Slurm hostlist expression contains hostname."""
    return any(
        _slurm_hostlist_token_contains(token, hostname)
        for token in _split_slurm_hostlist(hostlist)
    )


def _split_slurm_hostlist(hostlist: str) -> list[str]:
    """Split a Slurm hostlist on commas, ignoring commas inside brackets."""
    tokens: list[str] = []
    start: int = 0
    bracket_depth: int = 0

    for index, char in enumerate(hostlist):
        if char == "[":
            bracket_depth += 1
        elif char == "]" and bracket_depth > 0:
            bracket_depth -= 1
        elif char == "," and bracket_depth == 0:
            token: str = hostlist[start:index].strip()
            if token:
                tokens.append(token)
            start = index + 1

    token: str = hostlist[start:].strip()
    if token:
        tokens.append(token)

    return tokens


def _slurm_hostlist_token_contains(token: str, hostname: str) -> bool:
    """Return True when one Slurm hostlist token contains hostname."""
    bracket_start: int = token.find("[")
    if bracket_start == -1:
        return token == hostname

    bracket_end: int = token.find("]", bracket_start)
    if bracket_end == -1:
        return token == hostname

    prefix: str = token[:bracket_start]
    suffix: str = token[bracket_end + 1 :]
    if not hostname.startswith(prefix):
        return False

    remainder: str = hostname[len(prefix) :]
    return any(
        _slurm_hostlist_range_contains(part.strip(), suffix, remainder)
        for part in token[bracket_start + 1 : bracket_end].split(",")
        if part.strip()
    )


def _slurm_hostlist_range_contains(part: str, suffix: str, remainder: str) -> bool:
    """Return True when one bracket item can match the hostname remainder."""
    if "-" in part:
        start_value, end_value = part.split("-", 1)
    else:
        start_value = end_value = part

    if start_value.isdigit() and end_value.isdigit():
        return _slurm_numeric_range_contains(
            start_value,
            end_value,
            suffix,
            remainder,
        )

    return remainder.startswith(part) and _slurm_hostlist_token_contains(
        suffix,
        remainder[len(part) :],
    )


def _slurm_numeric_range_contains(
    start_value: str,
    end_value: str,
    suffix: str,
    remainder: str,
) -> bool:
    """Return True when a numeric hostlist range can match hostname remainder."""
    start: int = int(start_value)
    end: int = int(end_value)
    if start > end:
        return False

    padded_width: int = 0
    if _has_leading_zero_padding(start_value) or _has_leading_zero_padding(end_value):
        padded_width = max(len(start_value), len(end_value))

    if padded_width > 0:
        candidate: str = remainder[:padded_width]
        if len(candidate) != padded_width or not candidate.isdigit():
            return False
        return start <= int(candidate) <= end and _slurm_hostlist_token_contains(
            suffix, remainder[padded_width:]
        )

    digit_count: int = 0
    for char in remainder:
        if not char.isdigit():
            break
        digit_count += 1

    for length in range(1, digit_count + 1):
        candidate = remainder[:length]
        if len(candidate) > 1 and candidate.startswith("0"):
            continue
        if start <= int(candidate) <= end and _slurm_hostlist_token_contains(
            suffix,
            remainder[length:],
        ):
            return True

    return False


def _has_leading_zero_padding(value: str) -> bool:
    """Return True when a numeric range endpoint uses explicit zero padding."""
    return len(value) > 1 and value.startswith("0")


def wait_for_hostname_in_topology_conf(
    hostname: str,
    wait_timeout: int,
    poll_interval: int,
    topology_conf_path: Path = SLURM_TOPOLOGY_CONFIG_PATH,
) -> None:
    """Wait until topology.conf contains hostname, otherwise exit on timeout."""
    logger.info(
        "Waiting for hostname %s to appear in %s (timeout=%ds, poll=%ds)",
        hostname,
        topology_conf_path,
        wait_timeout,
        poll_interval,
    )
    start_time: float = time.monotonic()
    while True:
        elapsed: float = time.monotonic() - start_time
        if elapsed >= wait_timeout:
            logger.error(
                "Hostname %s not found in %s after %ds",
                hostname,
                topology_conf_path,
                wait_timeout,
            )
            sys.exit(1)

        if topology_conf_contains_hostname(topology_conf_path, hostname):
            logger.info("Hostname %s found in %s", hostname, topology_conf_path)
            return

        logger.info(
            "Hostname %s is not in %s yet, retrying... (%ds elapsed)",
            hostname,
            topology_conf_path,
            int(elapsed),
        )
        time.sleep(poll_interval)


def read_topology_for_node(topology_path: Path, node_name: str) -> str:
    """
    Read topology data for the given node from the ConfigMap.

    ConfigMap is mounted as a directory where each key is a file.
    Returns the topology string or "" if not found.
    """
    node_file: Path = Path(topology_path) / node_name

    if not node_file.is_file():
        return ""

    try:
        topology: str = node_file.read_text().strip()
        return topology if topology else ""
    except (IOError, OSError) as e:
        logger.warning("Failed to read topology file %s: %s", node_file, e)
        return ""


def format_slurm_topology(
    topology: str, topology_plugin: str = TOPOLOGY_PLUGIN_TREE
) -> str:
    """
    Format topology string for Slurm --conf option.

    Input formats for topology/tree:
      - JSON: '{"tier-1":"switch1","tier-2":"rack1"}' -> "topology=default:root:rack1:switch1"
        (builds full switch hierarchy: highest tier first, leaf last)
      - "default:switch1" -> "topology=default:root:switch1"
      - "default:sw_root:s1:s2" -> "topology=default:sw_root:s1:s2" (intermediate switches already present)
      - "tier-0=block1,tier-1=rack1" -> "topology=default:root:rack1:block1"
      - "switch1" -> "topology=default:root:switch1"

    Input formats for topology/block:
      - JSON: '{"tier-0":"block1","tier-1":"rack1"}' -> "topology=default:block1"
      - "tier-0=block1,tier-1=rack1" -> "topology=default:block1"
      - "default:block1" -> "topology=default:block1"
      - "block1" -> "topology=default:block1"

    Slurm dynamic topology format: topology=<name>:<switch_near_root>:...<leaf_switch>
    Tiers are sorted descending so that the highest tier (closest to root/spine) comes
    first in the path and the lowest tier (leaf) comes last.

    Example with K8s labels tier-1=leaf, tier-2=spine:
      topology=default:root:spine:leaf

    Returns the formatted Slurm Topology string.

    See: https://slurm.schedmd.com/topology.html#dynamic_topo
    """
    if not topology:
        return ""

    topology: str = topology.strip()
    topology_plugin: str = (topology_plugin or TOPOLOGY_PLUGIN_TREE).strip().lower()
    block_topology: bool = topology_plugin == TOPOLOGY_PLUGIN_BLOCK

    if topology.startswith("{"):
        try:
            parts: dict[str, str] = json.loads(topology)

            if block_topology:
                return _format_block_topology(parts)

            return _format_tier_topology(parts)
        except json.JSONDecodeError:
            logger.warning("Failed to parse topology as JSON: %s", topology)

    # If already in format "name:switch" or "name:sw1:sw2:sw3", use as-is
    if ":" in topology and "=" not in topology:
        if block_topology:
            return f"topology={topology}"

        colon_parts: list[str] = topology.split(":")

        # "name:leaf" -> add root: "topology=name:root:leaf"
        # "name:sw1:sw2:sw3" -> already has intermediates, keep as-is
        if len(colon_parts) == 2:
            return f"topology={colon_parts[0]}:root:{colon_parts[1]}"

        return f"topology={topology}"

    # If in format "tier-0=switch1,tier-1=rack1", build switch hierarchy
    if "=" in topology:
        parts: dict[str, str] = _parse_key_value_topology(topology)

        if block_topology:
            return _format_block_topology(parts)

        return _format_tier_topology(parts)

    if block_topology:
        return f"topology=default:{topology}"

    return f"topology=default:root:{topology}"


def _parse_key_value_topology(topology: str) -> dict[str, str]:
    """Parse comma-separated topology key/value pairs."""
    parts: dict[str, str] = {}

    for item in topology.split(","):
        item: str = item.strip()
        if "=" in item:
            key, value = item.split("=", 1)
            parts[key.strip()] = value.strip()

    return parts


def _format_block_topology(parts: dict[str, str]) -> str:
    """
    Format tier-based topology for topology/block.

    For block topology the dynamic topology unit is the block name, so the
    node must join tier-0 directly instead of a tree path through higher tiers.
    """
    if not parts or not isinstance(parts, dict):
        return ""

    block_name: str | None = (
        parts.get("tier-0") or parts.get("block") or parts.get("BlockName")
    )
    if not block_name:
        logger.warning("Failed to find tier-0 block name in topology data: %s", parts)
        return ""

    return f"topology=default:{block_name}"


def _format_tier_topology(parts: dict[str, str]) -> str:
    """
    Format tier-based topology from a dictionary.

    Args:
        parts: Dictionary with tier keys like {"tier-1": "switch1", "tier-2": "rack1"}

    Returns:
        Formatted Slurm Topology string with the full switch hierarchy.
        Tiers are ordered from highest number (spine/root-side) down to lowest (leaf),
        so that slurmctld can build the correct switch tree dynamically.

    See: https://slurm.schedmd.com/topology.html#dynamic_topo
         Format: Topology=<name>:<switch_near_root>:...:<leaf_switch>

    Example:
      - {"tier-1": "leaf00"} -> "topology=default:root:leaf00"
      - {"tier-1": "leaf00", "tier-2": "spine00"} -> "topology=default:root:spine00:leaf00"
      - {"tier-0": "block1", "tier-1": "rack1"} -> "topology=default:root:rack1:block1"
    """
    if not parts:
        return ""

    # Find all tier keys and their numbers
    tier_keys: list[tuple[int, str]] = []
    for k in parts.keys():
        if k.startswith("tier-"):
            try:
                tier_num: int = int(k.split("-")[1])
                tier_keys.append((tier_num, k))
            except (ValueError, IndexError):
                continue

    if tier_keys:
        # Sort descending: highest tier first (spine/root-side), lowest last (leaf)
        tier_keys.sort(key=lambda x: x[0], reverse=True)
        switches: list[str] = [parts[k] for _, k in tier_keys]
        return f"topology=default:root:{':'.join(switches)}"

    if parts:
        first_value: str = next(iter(parts.values()))
        return f"topology=default:root:{first_value}"

    return ""


def apply_node_topology(hostname: str, topology: str) -> None:
    """Apply topology to a node via scontrol update."""
    try:
        node_addr: str = get_node_addr()
        cmd = [
            "scontrol",
            "update",
            f"nodename={hostname}",
            f"{node_addr}",
            f"{topology}",
            "state=UNDRAIN",
            "reason=",
            "comment=",
        ]
        logger.info("Running: %s", " ".join(cmd))
        result: subprocess.CompletedProcess[str] = subprocess.run(
            cmd,
            capture_output=True,
            text=True,
            timeout=30,
        )
        if result.returncode != 0:
            output: str = (result.stdout + result.stderr).strip()
            if "Invalid node name" in output:
                logger.warning(
                    "scontrol update: node %s not yet registered (dynamic node first start), skipping: %s",
                    hostname,
                    output,
                )
                return
            logger.error(
                "scontrol update failed (rc=%d): %s", result.returncode, output
            )
            sys.exit(1)

        logger.info("Topology applied successfully for worker %s", hostname)
    except subprocess.TimeoutExpired:
        logger.error("scontrol update timed out")
        sys.exit(1)
    except FileNotFoundError:
        logger.error("scontrol command not found")
        sys.exit(1)


def is_gpu_enabled() -> bool:
    """Return True if NODESET_GPU_ENABLED is set to 'true'."""
    return os.environ.get("NODESET_GPU_ENABLED", "") == "true"


def wait_for_topology() -> None:
    """Wait for topology data to become available for this node, then apply it via scontrol.

    For non-GPU nodes (NODESET_GPU_ENABLED != 'true'), skips ConfigMap lookup and
    immediately assigns the node to the generic 'unknown' topology unit defined
    in topology.conf.
    """
    hostname: str = get_from_env_required("HOSTNAME")

    wait_timeout: int = get_topology_wait_timeout()
    poll_interval: int = get_topology_poll_interval()
    topology_plugin: str = get_topology_plugin()

    if not is_gpu_enabled():
        topology: str = "topology=default:root:unknown"
        if topology_plugin == TOPOLOGY_PLUGIN_BLOCK:
            topology = "topology=default:unknown"

        logger.info(
            "NODESET_GPU_ENABLED is not set to 'true', "
            "assigning node %s to %s topology",
            hostname,
            topology,
        )
        wait_for_hostname_in_topology_conf(hostname, wait_timeout, poll_interval)
        apply_node_topology(hostname, topology)
        return

    node_name: str = get_node_name()
    topology_path: Path = get_topology_path()

    logger.info("Waiting for topology data for node: %s", node_name)
    logger.info("Topology ConfigMap path: %s", topology_path)
    logger.info("Timeout: %ds, Poll interval: %ds", wait_timeout, poll_interval)

    start_time: float = time.monotonic()
    raw_topology: str = ""

    while True:
        elapsed: float = time.monotonic() - start_time

        if elapsed >= wait_timeout:
            logger.error(
                "Topology for node %s not found after %ds", node_name, wait_timeout
            )
            try:
                if topology_path.is_dir():
                    node_files: list[str] = [
                        path.name
                        for path in topology_path.iterdir()
                        if not path.name.startswith(".")
                    ]
                    logger.error("Available nodes in ConfigMap: %s", node_files)
                else:
                    logger.error("Topology ConfigMap not mounted at %s", topology_path)
            except OSError as e:
                logger.debug(
                    "Failed to list topology directory %s: %s",
                    topology_path,
                    e,
                )
            sys.exit(1)

        if not topology_path.is_dir():
            logger.info(
                "Waiting for ConfigMap to be mounted... (%ds elapsed)", int(elapsed)
            )
            time.sleep(poll_interval)
            continue

        raw_topology: str = read_topology_for_node(topology_path, node_name)
        if raw_topology:
            logger.info("Found topology for node %s: %s", node_name, raw_topology)
            break

        logger.info(
            "Node %s not found in topology ConfigMap, retrying... (%ds elapsed)",
            node_name,
            int(elapsed),
        )
        time.sleep(poll_interval)

    topology: str = format_slurm_topology(raw_topology, topology_plugin)
    if not topology:
        logger.error("Failed to format topology from raw data: %s", raw_topology)
        sys.exit(1)

    wait_for_hostname_in_topology_conf(hostname, wait_timeout, poll_interval)
    apply_node_topology(hostname, topology)


# endregion Topology functions


def main():
    parser: argparse.ArgumentParser = argparse.ArgumentParser(
        description="Worker initialization tasks for Slurm",
    )
    parser.add_argument(
        "commands",
        nargs="+",
        choices=["wait-controller", "wait-topology"],
        help="One or more initialization commands to run sequentially. "
        "If both are specified, wait-controller is always executed first.",
    )

    args: argparse.Namespace = parser.parse_args()

    # Ensure wait-controller always runs first
    commands: list[str] = sorted(
        args.commands, key=lambda c: 0 if c == "wait-controller" else 1
    )

    for cmd in commands:
        if cmd == "wait-controller":
            wait_for_controller()
        elif cmd == "wait-topology":
            wait_for_topology()


if __name__ == "__main__":
    main()
