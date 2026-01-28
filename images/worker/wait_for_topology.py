#!/usr/bin/env python3
"""
Wait for topology data to become available for this node.

This script reads the mounted ConfigMap and waits until the current node's
topology data is available. Uses only Python standard library.

Environment Variables:
    K8S_NODE_NAME: Kubernetes node name (required, from Downward API)
    TOPOLOGY_CONFIGMAP_PATH: Path to mounted ConfigMap (default: /etc/slurm/topology-node-labels)
    TOPOLOGY_ENV_FILE: Output file for topology env (default: /tmp/slurm_topology.env)
    TOPOLOGY_WAIT_TIMEOUT: Max wait time in seconds (default: 180)
    TOPOLOGY_POLL_INTERVAL: Poll interval in seconds (default: 5)
"""

import json
import os
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


def get_node_name():
    """Get the Kubernetes node name from environment variable."""
    node_name = os.environ["K8S_NODE_NAME"]
    return node_name


def get_topology_path():
    """Get the path to the topology ConfigMap mount."""
    return os.environ.get("TOPOLOGY_CONFIGMAP_PATH", "/etc/slurm/topology-node-labels")


def get_wait_timeout():
    """Get the maximum wait timeout in seconds."""
    timeout = os.environ.get("TOPOLOGY_WAIT_TIMEOUT", "180")
    return int(timeout)


def get_poll_interval():
    """Get the poll interval in seconds."""
    interval = os.environ.get("TOPOLOGY_POLL_INTERVAL", "5")
    return int(interval)


def read_topology_for_node(topology_path, node_name):
    """
    Read topology data for the given node from the ConfigMap.
    
    ConfigMap is mounted as a directory where each key is a file.
    Returns the topology string or None if not found.
    """
    node_file = os.path.join(topology_path, node_name)
    
    if not os.path.isfile(node_file):
        return None
    
    try:
        with open(node_file, "r") as f:
            topology = f.read().strip()
            return topology if topology else None
    except (IOError, OSError) as e:
        logger.warning("Failed to read topology file %s: %s", node_file, e)
        return None


def write_topology_env(topology):
    """
    Write topology to a file that can be sourced by the entrypoint.
    
    Format follows Slurm dynamic topology: Topology=<topo-name>:<switch>
    Example: Topology=default:switch1
    """
    env_file = os.environ.get("TOPOLOGY_ENV_FILE", "/tmp/slurm_topology.env")
    
    # Parse topology and format for Slurm
    # Expected input format from ConfigMap: "default:switch1" or "tier-0=switch1,tier-1=rack1"
    slurm_topology = format_slurm_topology(topology)
    
    try:
        with open(env_file, "w") as f:
            f.write(f"SLURM_NODE_TOPOLOGY=\"{slurm_topology}\"\n")
        logger.info("Topology written to %s: %s", env_file, slurm_topology)
        return True
    except (IOError, OSError) as e:
        logger.error("Failed to write topology env file: %s", e)
        return False


def format_slurm_topology(topology):
    """
    Format topology string for Slurm --conf option.
    
    Input formats:
      - JSON: '{"tier-1":"switch1","tier-2":"rack1"}' -> "Topology=default:switch1"
        (uses lowest tier number as the leaf switch/block)
      - "default:switch1" -> "Topology=default:switch1"
      - "default:sw_root:s1:s2" -> "Topology=default:sw_root:s1:s2" (intermediate switches)
      - "tier-0=block1,tier-1=rack1" -> "Topology=default:block1"
      - "switch1" -> "Topology=default:switch1"
    
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
        return f"Topology={topology}"
    
    # If in format "tier-0=switch1,tier-1=rack1", build switch hierarchy
    if "=" in topology:
        parts = {}
        for item in topology.split(","):
            item = item.strip()
            if "=" in item:
                key, value = item.split("=", 1)
                parts[key.strip()] = value.strip()
        return _format_tier_topology(parts)
    
    return f"Topology=default:{topology}"


def _format_tier_topology(parts):
    """
    Format tier-based topology from a dictionary.
    
    Args:
        parts: Dictionary with tier keys like {"tier-1": "switch1", "tier-2": "rack1"}
    
    Returns:
        Formatted Slurm Topology string using the lowest tier as the leaf switch/block.
        
    For dynamic topology in Slurm, we only need to specify the leaf switch (lowest tier).
    The slurmctld already knows the topology structure from topology.conf.
    
    Example: 
      - {"tier-0": "block1", "tier-1": "rack1"} -> "Topology=default:block1"
      - {"tier-1": "leaf00", "tier-2": "spine00"} -> "Topology=default:leaf00"
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
        return f"Topology=default:{leaf_switch}"
    
    if parts:
        first_value = next(iter(parts.values()))
        return f"Topology=default:{first_value}"
    
    return ""


def main():
    node_name = get_node_name()
    topology_path = get_topology_path()
    wait_timeout = get_wait_timeout()
    poll_interval = get_poll_interval()
    
    logger.info("Waiting for topology data for node: %s", node_name)
    logger.info("Topology ConfigMap path: %s", topology_path)
    logger.info("Timeout: %ds, Poll interval: %ds", wait_timeout, poll_interval)
    
    start_time = time.time()
    
    while True:
        elapsed = time.time() - start_time
        
        if not os.path.isdir(topology_path):
            if elapsed >= wait_timeout:
                logger.error("Topology ConfigMap not mounted at %s after %ds", topology_path, wait_timeout)
                sys.exit(1)
            logger.info("Waiting for ConfigMap to be mounted... (%ds elapsed)", int(elapsed))
            time.sleep(poll_interval)
            continue
        
        topology = read_topology_for_node(topology_path, node_name)
        
        if topology:
            logger.info("Found topology for node %s: %s", node_name, topology)
            if write_topology_env(topology):
                logger.info("Topology initialization complete")
                sys.exit(0)
            else:
                sys.exit(1)
        
        if elapsed >= wait_timeout:
            logger.error("Topology for node %s not found after %ds", node_name, wait_timeout)
            try:
                files = os.listdir(topology_path)
                node_files = [f for f in files if not f.startswith(".")]
                logger.error("Available nodes in ConfigMap: %s", node_files)
            except OSError as e:
                logger.debug(
                    "Failed to list topology directory %s when reporting available nodes: %s",
                    topology_path,
                    e,
                )
            sys.exit(1)
        
        logger.info("Node %s not found in topology ConfigMap, retrying... (%ds elapsed)", node_name, int(elapsed))
        time.sleep(poll_interval)


if __name__ == "__main__":
    main()
