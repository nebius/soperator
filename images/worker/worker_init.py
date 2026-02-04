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
"""

import argparse
import json
import os
import shutil
import subprocess
import sys
import time
import logging

# Configure logging
logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s - %(levelname)s - %(message)s",
    datefmt="%Y-%m-%d %H:%M:%S",
)
logger = logging.getLogger(__name__)

# Constants
SLURM_CONFIG_LINK_SOURCE = "/mnt/jail/etc/slurm"
SLURM_CONFIG_LINK_TARGET = "/etc/slurm"


# ============================================================
# Controller readiness functions
# ============================================================


def create_slurm_config_symlink() -> None:
    """Create symlink to Slurm configs from jail mount."""
    logger.info(
        "Creating symlink to slurm configs: %s -> %s",
        SLURM_CONFIG_LINK_TARGET,
        SLURM_CONFIG_LINK_SOURCE,
    )
    shutil.rmtree(SLURM_CONFIG_LINK_TARGET)
    os.symlink(SLURM_CONFIG_LINK_SOURCE, SLURM_CONFIG_LINK_TARGET)


def get_controller_max_attempts() -> int:
    """Get the maximum number of controller ping attempts."""
    return int(os.environ.get("CONTROLLER_MAX_ATTEMPTS", "60"))


def get_controller_poll_interval() -> int:
    """Get the poll interval in seconds for controller readiness checks."""
    return int(os.environ.get("CONTROLLER_POLL_INTERVAL", "5"))


def get_hostname() -> str:
    """Get the hostname of the current node."""
    return os.environ.get("HOSTNAME")


def get_node_addr() -> str:
    """Construct the NodeAddr for scontrol update based on environment variables.""" 
    pod_name = os.environ.get("K8S_POD_NAME")
    logger.info("Using K8S_POD_NAME=%s for NodeAddr construction", pod_name)
    service_name = os.environ.get("K8S_SERVICE_NAME") 
    logger.info("Using K8S_SERVICE_NAME=%s for NodeAddr construction", service_name)
    pod_namespace = os.environ.get("K8S_POD_NAMESPACE") 
    logger.info("Using K8S_POD_NAMESPACE=%s for NodeAddr construction", pod_namespace)
    if not pod_name or not service_name or not pod_namespace: 
        logger.error( "Environment variables K8S_POD_NAME, K8S_SERVICE_NAME, and K8S_POD_NAMESPACE must be set" ) 
        sys.exit(1) 
    node_addr = f"nodeaddr={pod_name}.{service_name}.{pod_namespace}.svc" 
    return node_addr


def wait_for_controller() -> None:
    """Wait for slurmctld to be ready by polling scontrol ping."""
    max_attempts = get_controller_max_attempts()
    poll_interval = get_controller_poll_interval()

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
            result = subprocess.run(
                ["scontrol", "ping", "--json"],
                capture_output=True,
                text=True,
                timeout=30,
            )
            if result.returncode == 0:
                try:
                    data = json.loads(result.stdout)
                    pings = data.get("pings", [])
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
                output = (result.stdout + result.stderr).strip()
                logger.warning("scontrol ping failed with output:\n%s", output)
        except subprocess.TimeoutExpired:
            logger.warning("scontrol ping timed out")
        except FileNotFoundError:
            logger.error("scontrol command not found")
            sys.exit(1)

        time.sleep(poll_interval)

    logger.error(
        "Controller did not become ready after %d attempts", max_attempts
    )
    sys.exit(1)


# ============================================================
# Topology functions
# ============================================================


def get_node_name() -> str:
    """Get the Kubernetes node name from environment variable."""
    node_name = os.environ["K8S_NODE_NAME"]
    return node_name


def get_topology_path() -> str:
    """Get the path to the topology ConfigMap mount."""
    return os.environ.get("TOPOLOGY_CONFIGMAP_PATH", "/tmp/slurm/topology-node-labels")


def get_topology_wait_timeout() -> int:
    """Get the maximum wait timeout in seconds."""
    timeout = os.environ.get("TOPOLOGY_WAIT_TIMEOUT", "180")
    return int(timeout)


def get_topology_poll_interval() -> int:
    """Get the poll interval in seconds."""
    interval = os.environ.get("TOPOLOGY_POLL_INTERVAL", "5")
    return int(interval)


def read_topology_for_node(topology_path: str, node_name: str) -> str:
    """
    Read topology data for the given node from the ConfigMap.

    ConfigMap is mounted as a directory where each key is a file.
    Returns the topology string or "" if not found.
    """
    node_file = os.path.join(topology_path, node_name)

    if not os.path.isfile(node_file):
        return ""

    try:
        with open(node_file, "r") as f:
            topology = f.read().strip()
            return topology if topology else ""
    except (IOError, OSError) as e:
        logger.warning("Failed to read topology file %s: %s", node_file, e)
        return ""

def format_slurm_topology(topology: str) -> str:
    """
    Format topology string for Slurm --conf option.

    Input formats:
      - JSON: '{"tier-1":"switch1","tier-2":"rack1"}' -> "topology=default:root:switch1"
        (uses lowest tier number as the leaf switch/block)
      - "default:switch1" -> "topology=default:root:switch1"
      - "default:sw_root:s1:s2" -> "topology=default:sw_root:s1:s2" (intermediate switches already present)
      - "tier-0=block1,tier-1=rack1" -> "topology=default:root:block1"
      - "switch1" -> "topology=default:root:switch1"

    For tree topology: the lowest tier (tier-0 or tier-1) is the leaf switch.
    For block topology: tier-0 is the block name.

    Returns the formatted Slurm Topology string.

    See: https://slurm.schedmd.com/topology.html#dynamic_topo
    """
    if not topology:
        return ""

    topology = topology.strip()

    if topology.startswith("{"):
        try:
            parts = json.loads(topology)
            return _format_tier_topology(parts)
        except json.JSONDecodeError:
            logger.warning("Failed to parse topology as JSON: %s", topology)

    # If already in format "name:switch" or "name:sw1:sw2:sw3", use as-is
    if ":" in topology and "=" not in topology:
        colon_parts = topology.split(":")
        # "name:leaf" -> add root: "topology=name:root:leaf"
        # "name:sw1:sw2:sw3" -> already has intermediates, keep as-is
        if len(colon_parts) == 2:
            return f"topology={colon_parts[0]}:root:{colon_parts[1]}"
        return f"topology={topology}"

    # If in format "tier-0=switch1,tier-1=rack1", build switch hierarchy
    if "=" in topology:
        parts = {}
        for item in topology.split(","):
            item = item.strip()
            if "=" in item:
                key, value = item.split("=", 1)
                parts[key.strip()] = value.strip()
        return _format_tier_topology(parts)

    return f"topology=default:root:{topology}"


def _format_tier_topology(parts: dict) -> str:
    """
    Format tier-based topology from a dictionary.

    Args:
        parts: Dictionary with tier keys like {"tier-1": "switch1", "tier-2": "rack1"}

    Returns:
        Formatted Slurm Topology string using the lowest tier as the leaf switch/block.

    For dynamic topology in Slurm, we only need to specify the leaf switch (lowest tier).
    The slurmctld already knows the topology structure from topology.conf.

    Example:
      - {"tier-0": "block1", "tier-1": "rack1"} -> "topology=default:root:block1"
      - {"tier-1": "leaf00", "tier-2": "spine00"} -> "topology=default:root:leaf00"
    """
    if not parts:
        return ""

    # Find all tier keys and their numbers
    tier_keys = []
    for k in parts.keys():
        if k.startswith("tier-"):
            try:
                tier_num = int(k.split("-")[1])
                tier_keys.append((tier_num, k))
            except (ValueError, IndexError):
                continue

    if tier_keys:
        tier_keys.sort(key=lambda x: x[0])
        lowest_tier_key = tier_keys[0][1]
        leaf_switch = parts[lowest_tier_key]
        return f"topology=default:root:{leaf_switch}"

    if parts:
        first_value = next(iter(parts.values()))
        return f"topology=default:root:{first_value}"

    return ""

def apply_node_topology(hostname: str, topology: str) -> None:
    """Apply topology to a node via scontrol update."""
    try:
        node_addr = get_node_addr()
        cmd = ["scontrol", "update", f"nodename={hostname}", f"{node_addr}", f"{topology}" ]
        logger.info("Running: %s", " ".join(cmd))
        result = subprocess.run(
            cmd,
            capture_output=True,
            text=True,
            timeout=30,
        )
        if result.returncode != 0:
            output = (result.stdout + result.stderr).strip()
            logger.error("scontrol update failed (rc=%d): %s", result.returncode, output)
            sys.exit(1)

        logger.info("Topology applied successfully for worker %s", hostname)
    except subprocess.TimeoutExpired:
        logger.error("scontrol update timed out")
        sys.exit(1)
    except FileNotFoundError:
        logger.error("scontrol command not found")
        sys.exit(1)

def wait_for_topology() -> None:
    """Wait for topology data to become available for this node, then apply it via scontrol."""
    node_name = get_node_name()
    topology_path = get_topology_path()
    wait_timeout = get_topology_wait_timeout()
    poll_interval = get_topology_poll_interval()

    logger.info("Waiting for topology data for node: %s", node_name)
    logger.info("Topology ConfigMap path: %s", topology_path)
    logger.info("Timeout: %ds, Poll interval: %ds", wait_timeout, poll_interval)

    start_time = time.time()
    raw_topology = ""

    while True:
        elapsed = time.time() - start_time

        if elapsed >= wait_timeout:
            logger.error(
                "Topology for node %s not found after %ds", node_name, wait_timeout
            )
            try:
                if os.path.isdir(topology_path):
                    files = os.listdir(topology_path)
                    node_files = [f for f in files if not f.startswith(".")]
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

        if not os.path.isdir(topology_path):
            logger.info(
                "Waiting for ConfigMap to be mounted... (%ds elapsed)", int(elapsed)
            )
            time.sleep(poll_interval)
            continue

        raw_topology = read_topology_for_node(topology_path, node_name)

        if raw_topology:
            logger.info("Found topology for node %s: %s", node_name, raw_topology)
            break

        logger.info(
            "Node %s not found in topology ConfigMap, retrying... (%ds elapsed)",
            node_name,
            int(elapsed),
        )
        time.sleep(poll_interval)

    topology = format_slurm_topology(raw_topology)
    if not topology:
        logger.error("Failed to format topology from raw data: %s", raw_topology)
        sys.exit(1)

    hostname = get_hostname()
    if not hostname:
        logger.error("HOSTNAME environment variable is not set")
        sys.exit(1)

    apply_node_topology(hostname, topology)


def main():
    parser = argparse.ArgumentParser(
        description="Worker initialization tasks for Slurm",
    )
    parser.add_argument(
        "commands",
        nargs="+",
        choices=["wait-controller", "wait-topology"],
        help="One or more initialization commands to run sequentially. "
        "If both are specified, wait-controller is always executed first.",
    )

    args = parser.parse_args()

    # Ensure wait-controller always runs first
    commands = sorted(args.commands, key=lambda c: 0 if c == "wait-controller" else 1)

    for cmd in commands:
        if cmd == "wait-controller":
            wait_for_controller()
        elif cmd == "wait-topology":
            wait_for_topology()


if __name__ == "__main__":
    main()
