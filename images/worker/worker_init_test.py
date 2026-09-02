#!/usr/bin/env python3
"""
Unit tests for worker_init.py

Run with: python3 -m pytest worker_init_test.py -v
Or without pytest: python3 worker_init_test.py
"""

import json
import os
import tempfile
import unittest
import unittest.mock as mock
from pathlib import Path

# Import the module under test
import worker_init


class TestFormatSlurmTopology(unittest.TestCase):
    """Tests for format_slurm_topology function."""

    def test_empty_topology(self):
        """Empty string returns empty string."""
        result = worker_init.format_slurm_topology("")
        self.assertEqual(result, "")

    def test_none_topology(self):
        """None returns empty string."""
        result = worker_init.format_slurm_topology(None)
        self.assertEqual(result, "")

    def test_simple_switch_name(self):
        """Simple switch name is formatted with default topology and root."""
        result = worker_init.format_slurm_topology("switch1")
        self.assertEqual(result, "topology=default:root:switch1")

    def test_topology_with_name_and_switch(self):
        """Format 'name:switch' gets root inserted."""
        result = worker_init.format_slurm_topology("default:switch1")
        self.assertEqual(result, "topology=default:root:switch1")

    def test_topology_with_intermediate_switches(self):
        """Format 'name:sw1:sw2:sw3' is preserved (already has intermediates)."""
        result = worker_init.format_slurm_topology("default:sw_root:s1:s2")
        self.assertEqual(result, "topology=default:sw_root:s1:s2")

    def test_custom_topology_name(self):
        """Custom topology name gets root inserted."""
        result = worker_init.format_slurm_topology("my-topo:leaf-switch")
        self.assertEqual(result, "topology=my-topo:root:leaf-switch")

    def test_tier_zero_alone_yields_no_tree_unit(self):
        """tier-0 names a block, not a switch, so a tree topology has nowhere to put the node."""
        result = worker_init.format_slurm_topology("tier-0=switch1")
        self.assertEqual(result, "")

    def test_tier_format_two_tiers(self):
        """Two tier format builds full hierarchy: spine first, leaf last."""
        result = worker_init.format_slurm_topology("tier-1=leaf01,tier-2=spine01")
        # tier-2 (spine, closer to root) first, tier-1 (leaf) last
        self.assertEqual(result, "topology=default:root:spine01:leaf01")

    def test_tier_format_three_tiers(self):
        """Three tier format builds full hierarchy from top switch to leaf."""
        result = worker_init.format_slurm_topology(
            "tier-1=leaf01,tier-2=spine01,tier-3=fabric01"
        )
        # tier-3 first, tier-2 second, tier-1 (leaf) last
        self.assertEqual(result, "topology=default:root:fabric01:spine01:leaf01")

    def test_tier_format_with_spaces(self):
        """Tier format with spaces is handled correctly."""
        result = worker_init.format_slurm_topology("tier-1 = leaf01 , tier-2 = spine01")
        self.assertEqual(result, "topology=default:root:spine01:leaf01")

    def test_tier_format_unordered(self):
        """Tier format with unordered tiers builds correct hierarchy."""
        result = worker_init.format_slurm_topology(
            "tier-2=spine01,tier-1=leaf01,tier-3=fabric01"
        )
        # Must be sorted: tier-3, tier-2, tier-1 regardless of input order
        self.assertEqual(result, "topology=default:root:fabric01:spine01:leaf01")

    def test_json_format_two_tiers(self):
        """JSON format with two tiers builds full hierarchy: spine first, leaf last."""
        result = worker_init.format_slurm_topology(
            '{"tier-1":"4dcbe855beb5ce19f484ba1a8960929d","tier-2":"5df641bb92d51e0dd5d97037fc7e2971"}'
        )
        # tier-2 (spine) first, tier-1 (leaf) last
        self.assertEqual(
            result,
            "topology=default:root:5df641bb92d51e0dd5d97037fc7e2971:4dcbe855beb5ce19f484ba1a8960929d",
        )

    def test_json_format_single_tier(self):
        """JSON format with single tier is parsed correctly."""
        result = worker_init.format_slurm_topology('{"tier-1":"leaf01"}')
        self.assertEqual(result, "topology=default:root:leaf01")

    def test_json_format_three_tiers(self):
        """JSON format with three tiers builds full hierarchy."""
        result = worker_init.format_slurm_topology(
            '{"tier-1":"leaf01","tier-2":"spine01","tier-3":"fabric01"}'
        )
        # tier-3 first, tier-2 second, tier-1 (leaf) last
        self.assertEqual(result, "topology=default:root:fabric01:spine01:leaf01")

    def test_json_format_with_whitespace(self):
        """JSON format with whitespace is handled correctly."""
        result = worker_init.format_slurm_topology(
            '  {"tier-1": "leaf01", "tier-2": "spine01"}  '
        )
        self.assertEqual(result, "topology=default:root:spine01:leaf01")

    def test_json_format_ignores_tier_zero_in_tree_mode(self):
        """tier-0 names a block, so the tree path stops at tier-1.

        The operator leaves tier-0 out of the tree it writes into the topology config, so
        including it here would put the node one switch below where the config places it.
        """
        result = worker_init.format_slurm_topology(
            '{"tier-0":"nvl0","tier-1":"leaf01"}'
        )
        self.assertEqual(result, "topology=default:root:leaf01")

    def test_json_format_block_topology_uses_tier_zero(self):
        """JSON format in block mode uses tier-0 as the block name."""
        result = worker_init.format_slurm_topology(
            '{"tier-0":"block1","tier-1":"leaf01","tier-2":"spine01"}',
            worker_init.TOPOLOGY_PLUGIN_BLOCK,
        )
        self.assertEqual(result, "topology=default:block1")

    def test_tier_format_block_topology_uses_tier_zero(self):
        """Key/value format in block mode uses tier-0 as the block name."""
        result = worker_init.format_slurm_topology(
            "tier-0=block1,tier-1=leaf01,tier-2=spine01",
            worker_init.TOPOLOGY_PLUGIN_BLOCK,
        )
        self.assertEqual(result, "topology=default:block1")

    def test_fabric_is_top_switch_for_tier_path(self):
        """A configured fabric is used as the top-of-tree switch instead of "root"."""
        result = worker_init.format_slurm_topology(
            '{"tier-1":"leaf01","tier-2":"spine01"}',
            worker_init.TOPOLOGY_PLUGIN_TREE,
            "fab-a",
        )
        self.assertEqual(result, "topology=default:fab-a:spine01:leaf01")

    def test_fabric_key_value_and_bare_forms(self):
        """The fabric also applies to key/value and bare-name inputs."""
        self.assertEqual(
            worker_init.format_slurm_topology(
                "tier-1=leaf01", worker_init.TOPOLOGY_PLUGIN_TREE, "fab-a"
            ),
            "topology=default:fab-a:leaf01",
        )
        self.assertEqual(
            worker_init.format_slurm_topology(
                "leaf01", worker_init.TOPOLOGY_PLUGIN_TREE, "fab-a"
            ),
            "topology=default:fab-a:leaf01",
        )

    def test_fabric_empty_defaults_to_root(self):
        """An empty/whitespace fabric falls back to the legacy "root" top switch."""
        result = worker_init.format_slurm_topology(
            '{"tier-1":"leaf01"}', worker_init.TOPOLOGY_PLUGIN_TREE, "  "
        )
        self.assertEqual(result, "topology=default:root:leaf01")

    def test_fabric_ignored_for_block_topology(self):
        """Block topology has no root hierarchy, so the fabric is not applied."""
        result = worker_init.format_slurm_topology(
            '{"tier-0":"block1"}', worker_init.TOPOLOGY_PLUGIN_BLOCK, "fab-a"
        )
        self.assertEqual(result, "topology=default:block1")

    def test_named_block_topology_preserves_topology_name(self):
        """Block topology with an explicit topology name is preserved."""
        result = worker_init.format_slurm_topology(
            "default:block1",
            worker_init.TOPOLOGY_PLUGIN_BLOCK,
        )
        self.assertEqual(result, "topology=default:block1")

    def test_simple_block_topology_name(self):
        """Simple block name gets the default topology name."""
        result = worker_init.format_slurm_topology(
            "block1",
            worker_init.TOPOLOGY_PLUGIN_BLOCK,
        )
        self.assertEqual(result, "topology=default:block1")

    def test_block_topology_requires_tier_zero_for_structured_data(self):
        """Structured block data without tier-0 is rejected."""
        result = worker_init.format_slurm_topology(
            '{"tier-1":"leaf01"}',
            worker_init.TOPOLOGY_PLUGIN_BLOCK,
        )
        self.assertEqual(result, "")


class TestFormatTierTopology(unittest.TestCase):
    """Tests for _format_tier_topology internal function."""

    def test_empty_dict(self):
        """Empty dictionary returns empty string."""
        result = worker_init._format_tier_topology({})
        self.assertEqual(result, "")

    def test_none_input(self):
        """None input returns empty string."""
        result = worker_init._format_tier_topology(None)
        self.assertEqual(result, "")

    def test_single_tier(self):
        """Single tier returns that tier value with root."""
        result = worker_init._format_tier_topology({"tier-1": "switch1"})
        self.assertEqual(result, "topology=default:root:switch1")

    def test_two_tiers_builds_hierarchy(self):
        """Two tiers builds full hierarchy: spine first, leaf last."""
        result = worker_init._format_tier_topology(
            {"tier-1": "leaf01", "tier-2": "spine01"}
        )
        self.assertEqual(result, "topology=default:root:spine01:leaf01")

    def test_tier_zero_excluded_from_tree(self):
        """tier-0 is the NVL/block domain and is not part of the switch tree."""
        result = worker_init._format_tier_topology(
            {"tier-0": "nvl0", "tier-1": "leaf01", "tier-2": "spine01"}
        )
        self.assertEqual(result, "topology=default:root:spine01:leaf01")

    def test_three_tiers_builds_hierarchy(self):
        """Three tiers builds full path from fabric to leaf."""
        result = worker_init._format_tier_topology(
            {"tier-1": "leaf01", "tier-2": "spine01", "tier-3": "fabric01"}
        )
        self.assertEqual(result, "topology=default:root:fabric01:spine01:leaf01")

    def test_unordered_tier_keys(self):
        """Tier keys in any order produce the same sorted hierarchy."""
        result = worker_init._format_tier_topology(
            {"tier-3": "fabric01", "tier-1": "leaf01", "tier-2": "spine01"}
        )
        self.assertEqual(result, "topology=default:root:fabric01:spine01:leaf01")

    def test_non_tier_keys_ignored(self):
        """Non-tier keys are ignored, tier keys used."""
        result = worker_init._format_tier_topology(
            {"other": "value", "tier-1": "leaf01", "name": "test"}
        )
        self.assertEqual(result, "topology=default:root:leaf01")

    def test_only_non_tier_keys_uses_first_value(self):
        """Only non-tier keys uses first value as fallback."""
        result = worker_init._format_tier_topology({"switch": "sw1", "rack": "r1"})
        self.assertEqual(result, "topology=default:root:sw1")

    def test_invalid_tier_format_ignored(self):
        """Invalid tier format keys are ignored."""
        result = worker_init._format_tier_topology(
            {"tier-abc": "invalid", "tier-1": "leaf01"}
        )
        self.assertEqual(result, "topology=default:root:leaf01")

    def test_tier_with_hash_value(self):
        """Tier with hash value (real ConfigMap data) builds full hierarchy."""
        result = worker_init._format_tier_topology(
            {
                "tier-1": "4dcbe855beb5ce19f484ba1a8960929d",
                "tier-2": "5df641bb92d51e0dd5d97037fc7e2971",
            }
        )
        # tier-2 (spine) first, tier-1 (leaf) last
        self.assertEqual(
            result,
            "topology=default:root:5df641bb92d51e0dd5d97037fc7e2971:4dcbe855beb5ce19f484ba1a8960929d",
        )


class TestReadTopologyForNode(unittest.TestCase):
    """Tests for read_topology_for_node function."""

    def setUp(self):
        """Create a temporary directory for test files."""
        self.temp_dir = tempfile.mkdtemp()

    def tearDown(self):
        """Clean up temporary directory."""
        import shutil

        shutil.rmtree(self.temp_dir)

    def test_read_existing_node(self):
        """Reading topology for existing node returns content."""
        node_name = "node-001"
        topology = "default:switch1"

        # Create node file
        node_file = os.path.join(self.temp_dir, node_name)
        with open(node_file, "w") as f:
            f.write(topology)

        result = worker_init.read_topology_for_node(self.temp_dir, node_name)
        self.assertEqual(result, topology)

    def test_read_nonexistent_node(self):
        """Reading topology for non-existent node returns empty string."""
        result = worker_init.read_topology_for_node(self.temp_dir, "nonexistent")
        self.assertEqual(result, "")

    def test_read_empty_file(self):
        """Reading empty file returns empty string."""
        node_name = "empty-node"
        node_file = os.path.join(self.temp_dir, node_name)
        with open(node_file, "w") as f:
            f.write("")

        result = worker_init.read_topology_for_node(self.temp_dir, node_name)
        self.assertEqual(result, "")

    def test_read_whitespace_only(self):
        """Reading whitespace-only file returns empty string."""
        node_name = "whitespace-node"
        node_file = os.path.join(self.temp_dir, node_name)
        with open(node_file, "w") as f:
            f.write("   \n\t  \n")

        result = worker_init.read_topology_for_node(self.temp_dir, node_name)
        self.assertEqual(result, "")

    def test_read_strips_whitespace(self):
        """Reading file strips leading/trailing whitespace."""
        node_name = "node-with-whitespace"
        node_file = os.path.join(self.temp_dir, node_name)
        with open(node_file, "w") as f:
            f.write("  default:switch1  \n")

        result = worker_init.read_topology_for_node(self.temp_dir, node_name)
        self.assertEqual(result, "default:switch1")


class TestGetEnvironmentVariables(unittest.TestCase):
    """Tests for environment variable getter functions."""

    def test_get_node_name_set(self):
        """Get node name when environment variable is set."""
        with mock.patch.dict(os.environ, {"K8S_NODE_NAME": "test-node-001"}):
            result = worker_init.get_node_name()
        self.assertEqual(result, "test-node-001")

    def test_get_node_name_not_set(self):
        """Get node name when environment variable is not set raises KeyError."""
        env = os.environ.copy()
        env.pop("K8S_NODE_NAME", None)
        with mock.patch.dict(os.environ, env, clear=True):
            with self.assertRaises(KeyError):
                worker_init.get_node_name()

    def test_get_topology_fabric_default(self):
        """Get fabric returns "root" when the env var is not set."""
        env = os.environ.copy()
        env.pop("SLURM_TOPOLOGY_FABRIC", None)
        with mock.patch.dict(os.environ, env, clear=True):
            result = worker_init.get_topology_fabric()
        self.assertEqual(result, "root")

    def test_get_topology_fabric_custom(self):
        """Get fabric returns the configured value when set."""
        with mock.patch.dict(os.environ, {"SLURM_TOPOLOGY_FABRIC": "fab-a"}):
            result = worker_init.get_topology_fabric()
        self.assertEqual(result, "fab-a")

    def test_get_topology_fabric_empty_defaults_to_root(self):
        """An empty fabric env var falls back to "root"."""
        with mock.patch.dict(os.environ, {"SLURM_TOPOLOGY_FABRIC": "  "}):
            result = worker_init.get_topology_fabric()
        self.assertEqual(result, "root")

    def test_get_topology_path_default(self):
        """Get topology path returns default when not set."""
        env = os.environ.copy()
        env.pop("TOPOLOGY_CONFIGMAP_PATH", None)
        with mock.patch.dict(os.environ, env, clear=True):
            result = worker_init.get_topology_path()
        self.assertEqual(result, Path("/tmp/slurm/topology-node-labels"))

    def test_get_topology_path_custom(self):
        """Get topology path returns custom value when set."""
        with mock.patch.dict(os.environ, {"TOPOLOGY_CONFIGMAP_PATH": "/custom/path"}):
            result = worker_init.get_topology_path()
        self.assertEqual(result, Path("/custom/path"))

    def test_get_topology_wait_timeout_default(self):
        """Get wait timeout returns default when not set."""
        env = os.environ.copy()
        env.pop("TOPOLOGY_WAIT_TIMEOUT", None)
        with mock.patch.dict(os.environ, env, clear=True):
            result = worker_init.get_topology_wait_timeout()
        self.assertEqual(result, 180)

    def test_get_topology_wait_timeout_custom(self):
        """Get wait timeout returns custom value when set."""
        with mock.patch.dict(os.environ, {"TOPOLOGY_WAIT_TIMEOUT": "300"}):
            result = worker_init.get_topology_wait_timeout()
        self.assertEqual(result, 300)

    def test_get_topology_wait_timeout_invalid(self):
        """Get wait timeout raises ValueError for invalid value."""
        with mock.patch.dict(os.environ, {"TOPOLOGY_WAIT_TIMEOUT": "invalid"}):
            with self.assertRaises(ValueError):
                worker_init.get_topology_wait_timeout()

    def test_get_topology_poll_interval_default(self):
        """Get poll interval returns default when not set."""
        env = os.environ.copy()
        env.pop("TOPOLOGY_POLL_INTERVAL", None)
        with mock.patch.dict(os.environ, env, clear=True):
            result = worker_init.get_topology_poll_interval()
        self.assertEqual(result, 5)

    def test_get_topology_poll_interval_custom(self):
        """Get poll interval returns custom value when set."""
        with mock.patch.dict(os.environ, {"TOPOLOGY_POLL_INTERVAL": "10"}):
            result = worker_init.get_topology_poll_interval()
        self.assertEqual(result, 10)

    def test_get_controller_max_attempts_default(self):
        """Get controller max attempts returns default when not set."""
        env = os.environ.copy()
        env.pop("CONTROLLER_MAX_ATTEMPTS", None)
        with mock.patch.dict(os.environ, env, clear=True):
            result = worker_init.get_controller_max_attempts()
        self.assertEqual(result, 60)

    def test_get_controller_max_attempts_custom(self):
        """Get controller max attempts returns custom value when set."""
        with mock.patch.dict(os.environ, {"CONTROLLER_MAX_ATTEMPTS": "120"}):
            result = worker_init.get_controller_max_attempts()
        self.assertEqual(result, 120)

    def test_get_controller_poll_interval_default(self):
        """Get controller poll interval returns default when not set."""
        env = os.environ.copy()
        env.pop("CONTROLLER_POLL_INTERVAL", None)
        with mock.patch.dict(os.environ, env, clear=True):
            result = worker_init.get_controller_poll_interval()
        self.assertEqual(result, 5)

    def test_get_controller_poll_interval_custom(self):
        """Get controller poll interval returns custom value when set."""
        with mock.patch.dict(os.environ, {"CONTROLLER_POLL_INTERVAL": "10"}):
            result = worker_init.get_controller_poll_interval()
        self.assertEqual(result, 10)


class TestCreateSlurmConfigSymlink(unittest.TestCase):
    """Tests for create_slurm_config_symlink function."""

    def setUp(self):
        """Create temporary directories for test files."""
        self.temp_dir = tempfile.mkdtemp()
        self.source = os.path.join(self.temp_dir, "source_slurm")
        self.target = os.path.join(self.temp_dir, "target_slurm")
        os.makedirs(self.source)

    def tearDown(self):
        """Clean up temporary directories."""
        import shutil

        shutil.rmtree(self.temp_dir)

    def test_create_symlink_no_existing_target(self):
        """Creates symlink when target does not exist."""
        with mock.patch.object(
            worker_init, "SLURM_CONFIG_LINK_SOURCE", self.source
        ), mock.patch.object(worker_init, "SLURM_CONFIG_LINK_TARGET", self.target):
            worker_init.create_slurm_config_symlink()

        self.assertTrue(os.path.islink(self.target))
        self.assertEqual(os.readlink(self.target), self.source)

    def test_create_symlink_replaces_existing_symlink(self):
        """Replaces existing symlink at target."""
        os.symlink("/some/old/path", self.target)

        with mock.patch.object(
            worker_init, "SLURM_CONFIG_LINK_SOURCE", self.source
        ), mock.patch.object(worker_init, "SLURM_CONFIG_LINK_TARGET", self.target):
            worker_init.create_slurm_config_symlink()

        self.assertTrue(os.path.islink(self.target))
        self.assertEqual(os.readlink(self.target), self.source)

    def test_create_symlink_replaces_existing_directory(self):
        """Replaces existing directory at target."""
        os.makedirs(self.target)
        # Create a file inside to ensure rmtree works
        with open(os.path.join(self.target, "test.conf"), "w") as f:
            f.write("test")

        with mock.patch.object(
            worker_init, "SLURM_CONFIG_LINK_SOURCE", self.source
        ), mock.patch.object(worker_init, "SLURM_CONFIG_LINK_TARGET", self.target):
            worker_init.create_slurm_config_symlink()

        self.assertTrue(os.path.islink(self.target))
        self.assertEqual(os.readlink(self.target), self.source)


class TestWaitForController(unittest.TestCase):
    """Tests for wait_for_controller function."""

    _PING_UP_JSON = json.dumps(
        {
            "pings": [
                {
                    "hostname": "controller-0",
                    "responding": True,
                    "latency": 1912,
                    "primary": True,
                    "status": "No error",
                }
            ],
            "errors": [],
            "warnings": [],
        }
    )

    _PING_DOWN_JSON = json.dumps(
        {
            "pings": [
                {
                    "hostname": "controller-0",
                    "responding": False,
                    "latency": 30_000_000,
                    "primary": True,
                    "status": "Connection timed out",
                }
            ],
            "errors": [],
            "warnings": [],
        }
    )

    @mock.patch("worker_init.create_slurm_config_symlink")
    @mock.patch("subprocess.run")
    def test_controller_ready_immediately(self, mock_run, mock_symlink):
        """Controller is ready on first attempt."""
        mock_run.return_value = mock.Mock(
            returncode=0, stdout=self._PING_UP_JSON, stderr=""
        )

        with mock.patch.dict(
            os.environ,
            {"CONTROLLER_MAX_ATTEMPTS": "5", "CONTROLLER_POLL_INTERVAL": "0"},
        ):
            worker_init.wait_for_controller()

        mock_symlink.assert_called_once()
        mock_run.assert_called_once()

    @mock.patch("worker_init.create_slurm_config_symlink")
    @mock.patch("subprocess.run")
    @mock.patch("time.sleep")
    def test_controller_ready_after_retries(self, mock_sleep, mock_run, mock_symlink):
        """Controller becomes ready after several attempts."""
        mock_run.side_effect = [
            mock.Mock(returncode=1, stdout="", stderr="error"),
            mock.Mock(returncode=1, stdout="", stderr="error"),
            mock.Mock(returncode=0, stdout=self._PING_UP_JSON, stderr=""),
        ]

        with mock.patch.dict(
            os.environ,
            {"CONTROLLER_MAX_ATTEMPTS": "5", "CONTROLLER_POLL_INTERVAL": "1"},
        ):
            worker_init.wait_for_controller()

        self.assertEqual(mock_run.call_count, 3)
        self.assertEqual(mock_sleep.call_count, 2)

    @mock.patch("worker_init.create_slurm_config_symlink")
    @mock.patch("subprocess.run")
    @mock.patch("time.sleep")
    def test_controller_not_responding_retries(
        self, mock_sleep, mock_run, mock_symlink
    ):
        """Controller returns JSON but responding=false, retries until ready."""
        mock_run.side_effect = [
            mock.Mock(returncode=0, stdout=self._PING_DOWN_JSON, stderr=""),
            mock.Mock(returncode=0, stdout=self._PING_UP_JSON, stderr=""),
        ]

        with mock.patch.dict(
            os.environ,
            {"CONTROLLER_MAX_ATTEMPTS": "5", "CONTROLLER_POLL_INTERVAL": "0"},
        ):
            worker_init.wait_for_controller()

        self.assertEqual(mock_run.call_count, 2)

    @mock.patch("worker_init.create_slurm_config_symlink")
    @mock.patch("subprocess.run")
    @mock.patch("time.sleep")
    def test_controller_invalid_json_retries(self, mock_sleep, mock_run, mock_symlink):
        """Controller returns invalid JSON, retries."""
        mock_run.side_effect = [
            mock.Mock(returncode=0, stdout="not json", stderr=""),
            mock.Mock(returncode=0, stdout=self._PING_UP_JSON, stderr=""),
        ]

        with mock.patch.dict(
            os.environ,
            {"CONTROLLER_MAX_ATTEMPTS": "5", "CONTROLLER_POLL_INTERVAL": "0"},
        ):
            worker_init.wait_for_controller()

        self.assertEqual(mock_run.call_count, 2)

    @mock.patch("worker_init.create_slurm_config_symlink")
    @mock.patch("subprocess.run")
    @mock.patch("time.sleep")
    def test_controller_timeout(self, mock_sleep, mock_run, mock_symlink):
        """Controller does not become ready within max attempts."""
        mock_run.return_value = mock.Mock(
            returncode=1, stdout="", stderr="connection refused"
        )

        with mock.patch.dict(
            os.environ,
            {"CONTROLLER_MAX_ATTEMPTS": "3", "CONTROLLER_POLL_INTERVAL": "0"},
        ):
            with self.assertRaises(SystemExit) as ctx:
                worker_init.wait_for_controller()
            self.assertEqual(ctx.exception.code, 1)

        self.assertEqual(mock_run.call_count, 3)

    @mock.patch("worker_init.create_slurm_config_symlink")
    @mock.patch("subprocess.run")
    def test_controller_scontrol_not_found(self, mock_run, mock_symlink):
        """scontrol command not found exits immediately."""
        mock_run.side_effect = FileNotFoundError("scontrol not found")

        with mock.patch.dict(
            os.environ,
            {"CONTROLLER_MAX_ATTEMPTS": "5", "CONTROLLER_POLL_INTERVAL": "0"},
        ):
            with self.assertRaises(SystemExit) as ctx:
                worker_init.wait_for_controller()
            self.assertEqual(ctx.exception.code, 1)

    @mock.patch("worker_init.create_slurm_config_symlink")
    @mock.patch("subprocess.run")
    @mock.patch("time.sleep")
    def test_controller_ping_timeout(self, mock_sleep, mock_run, mock_symlink):
        """scontrol ping times out but retries."""
        import subprocess

        mock_run.side_effect = [
            subprocess.TimeoutExpired(cmd="scontrol", timeout=30),
            mock.Mock(returncode=0, stdout=self._PING_UP_JSON, stderr=""),
        ]

        with mock.patch.dict(
            os.environ,
            {"CONTROLLER_MAX_ATTEMPTS": "5", "CONTROLLER_POLL_INTERVAL": "0"},
        ):
            worker_init.wait_for_controller()

        self.assertEqual(mock_run.call_count, 2)

    @mock.patch("worker_init.create_slurm_config_symlink")
    @mock.patch("subprocess.run")
    @mock.patch("time.sleep")
    def test_controller_multiple_pings_all_must_respond(
        self, mock_sleep, mock_run, mock_symlink
    ):
        """All controllers in pings array must be responding."""
        partial_json = json.dumps(
            {
                "pings": [
                    {"hostname": "ctrl-0", "responding": True, "primary": True},
                    {"hostname": "ctrl-1", "responding": False, "primary": False},
                ],
                "errors": [],
                "warnings": [],
            }
        )
        all_up_json = json.dumps(
            {
                "pings": [
                    {"hostname": "ctrl-0", "responding": True, "primary": True},
                    {"hostname": "ctrl-1", "responding": True, "primary": False},
                ],
                "errors": [],
                "warnings": [],
            }
        )
        mock_run.side_effect = [
            mock.Mock(returncode=0, stdout=partial_json, stderr=""),
            mock.Mock(returncode=0, stdout=all_up_json, stderr=""),
        ]

        with mock.patch.dict(
            os.environ,
            {"CONTROLLER_MAX_ATTEMPTS": "5", "CONTROLLER_POLL_INTERVAL": "0"},
        ):
            worker_init.wait_for_controller()

        self.assertEqual(mock_run.call_count, 2)


class TestApplyNodeTopology(unittest.TestCase):
    """Tests for applying the prepared topology after controller readiness."""

    @mock.patch("worker_init.get_node_addr", return_value="nodeaddr=worker.service.svc")
    @mock.patch("subprocess.run")
    def test_updates_node_without_another_ping(self, mock_run, mock_node_addr):
        mock_run.return_value = mock.Mock(returncode=0, stdout="", stderr="")

        worker_init.apply_node_topology("worker-0", "topology=default:root:leaf-0")

        mock_run.assert_called_once_with(
            [
                "scontrol",
                "update",
                "nodename=worker-0",
                "nodeaddr=worker.service.svc",
                "topology=default:root:leaf-0",
            ],
            capture_output=True,
            text=True,
            timeout=30,
        )


class TestIsGpuEnabled(unittest.TestCase):
    """Tests for is_gpu_enabled function."""

    def test_gpu_enabled_true(self):
        """Returns True when NODESET_GPU_ENABLED=true."""
        with mock.patch.dict(os.environ, {"NODESET_GPU_ENABLED": "true"}):
            self.assertTrue(worker_init.is_gpu_enabled())

    def test_gpu_enabled_false(self):
        """Returns False when NODESET_GPU_ENABLED=false."""
        with mock.patch.dict(os.environ, {"NODESET_GPU_ENABLED": "false"}):
            self.assertFalse(worker_init.is_gpu_enabled())

    def test_gpu_enabled_not_set(self):
        """Returns False when NODESET_GPU_ENABLED is not set."""
        env = os.environ.copy()
        env.pop("NODESET_GPU_ENABLED", None)
        with mock.patch.dict(os.environ, env, clear=True):
            self.assertFalse(worker_init.is_gpu_enabled())

    def test_gpu_enabled_case_sensitive(self):
        """Returns False for 'True' (uppercase) - must be exactly 'true'."""
        with mock.patch.dict(os.environ, {"NODESET_GPU_ENABLED": "True"}):
            self.assertFalse(worker_init.is_gpu_enabled())



class TestTopologyConfigContainsHostname(unittest.TestCase):
    """Tests for topology.yaml hostname membership checks."""

    def test_exact_token(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            topology_config = Path(temp_dir) / "topology.yaml"
            with open(topology_config, "w") as f:
                f.write(
                    "- topology: tree-ib\n"
                    "  tree:\n"
                    "    switches:\n"
                    "        - switch: unknown\n"
                    "          nodes: worker-0,worker-1\n"
                )

            self.assertTrue(
                worker_init.topology_config_contains_hostname(
                    topology_config,
                    "worker-0",
                )
            )

    def test_does_not_match_partial_hostname(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            topology_config = Path(temp_dir) / "topology.yaml"
            with open(topology_config, "w") as f:
                f.write("- topology: tree-ib\n  tree:\n    switches:\n        - switch: unknown\n          nodes: worker-10\n")

            self.assertFalse(
                worker_init.topology_config_contains_hostname(
                    topology_config,
                    "worker-1",
                )
            )

    def test_contains_hostname_in_slurm_range(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            topology_config = Path(temp_dir) / "topology.yaml"
            with open(topology_config, "w") as f:
                f.write("- topology: tree-ib\n  tree:\n    switches:\n        - switch: unknown\n          nodes: worker-[0-5]\n")

            self.assertTrue(
                worker_init.topology_config_contains_hostname(
                    topology_config,
                    "worker-1",
                )
            )

    def test_contains_single_digit_hostname_in_unpadded_range_ending_with_two_digits(
        self,
    ):
        with tempfile.TemporaryDirectory() as temp_dir:
            topology_config = Path(temp_dir) / "topology.yaml"
            with open(topology_config, "w") as f:
                f.write(
                    "- topology: block-nvl72\n"
                    "  block:\n"
                    "    block_sizes:\n"
                    "        - 13\n"
                    "    blocks:\n"
                    "        - block: computenvlinstancegroup-e00nd50be1dk3g89f3\n"
                    "          nodes: worker-rack1-[0-12]\n"
                )

            self.assertTrue(
                worker_init.topology_config_contains_hostname(
                    topology_config,
                    "worker-rack1-8",
                )
            )

    def test_does_not_match_hostname_outside_slurm_range(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            topology_config = Path(temp_dir) / "topology.yaml"
            with open(topology_config, "w") as f:
                f.write("SwitchName=unknown Nodes=worker-[10-15]\n")

            self.assertFalse(
                worker_init.topology_config_contains_hostname(
                    topology_config,
                    "worker-1",
                )
            )

    def test_contains_hostname_in_merged_slurm_hostlist(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            topology_config = Path(temp_dir) / "topology.yaml"
            with open(topology_config, "w") as f:
                f.write(
                    "- topology: block-nvl72\n"
                    "  block:\n"
                    "    blocks:\n"
                    "        - block: block-a\n"
                    "          nodes: worker-[0-2,4],worker-cpu-[0-1],workerkek1\n"
                )

            self.assertTrue(
                worker_init.topology_config_contains_hostname(
                    topology_config,
                    "worker-cpu-1",
                )
            )
            self.assertTrue(
                worker_init.topology_config_contains_hostname(
                    topology_config,
                    "worker-4",
                )
            )
            self.assertFalse(
                worker_init.topology_config_contains_hostname(
                    topology_config,
                    "worker-3",
                )
            )
            self.assertTrue(
                worker_init.topology_config_contains_hostname(
                    topology_config,
                    "workerkek1",
                )
            )

    def test_contains_hostname_in_zero_padded_slurm_range(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            topology_config = Path(temp_dir) / "topology.yaml"
            with open(topology_config, "w") as f:
                f.write(
                    "- topology: tree-ib\n"
                    "  tree:\n"
                    "    switches:\n"
                    "        - switch: leaf\n"
                    "          nodes: gpu[099-101]\n"
                )

            self.assertTrue(
                worker_init.topology_config_contains_hostname(topology_config, "gpu099")
            )
            self.assertFalse(
                worker_init.topology_config_contains_hostname(topology_config, "gpu99")
            )

    def test_nodes_all_contains_hostname(self):
        with tempfile.TemporaryDirectory() as temp_dir:
            topology_config = Path(temp_dir) / "topology.yaml"
            with open(topology_config, "w") as f:
                f.write(
                    "- topology: tree-ib\n"
                    "  tree:\n"
                    "    switches:\n"
                    "        - switch: root\n"
                    "          nodes: ALL\n"
                )

            self.assertTrue(
                worker_init.topology_config_contains_hostname(
                    topology_config,
                    "worker-1",
                )
            )


class TestTopologyIntegration(unittest.TestCase):
    """Integration tests for the topology flow."""

    def setUp(self):
        """Create temporary directories for test files."""
        self.temp_dir = tempfile.mkdtemp()
        self.configmap_dir = os.path.join(self.temp_dir, "configmap")
        os.makedirs(self.configmap_dir)

    def tearDown(self):
        """Clean up temporary directories."""
        import shutil

        shutil.rmtree(self.temp_dir)

    def test_full_flow_read_and_format(self):
        """Test read topology then format builds full hierarchy."""
        node_name = "gpu-node-001"
        topology = "tier-1=leaf01,tier-2=spine01"

        # Create node topology file
        node_file = os.path.join(self.configmap_dir, node_name)
        with open(node_file, "w") as f:
            f.write(topology)

        # Read topology
        result = worker_init.read_topology_for_node(self.configmap_dir, node_name)
        self.assertEqual(result, topology)

        # Format topology - spine first, leaf last
        formatted = worker_init.format_slurm_topology(result)
        self.assertEqual(formatted, "topology=default:root:spine01:leaf01")

    def test_full_flow_json_input(self):
        """Test read JSON topology then format builds full hierarchy."""
        node_name = "gpu-node-002"
        topology = '{"tier-1": "leaf01", "tier-2": "spine01"}'

        node_file = os.path.join(self.configmap_dir, node_name)
        with open(node_file, "w") as f:
            f.write(topology)

        result = worker_init.read_topology_for_node(self.configmap_dir, node_name)
        self.assertEqual(result, topology)

        formatted = worker_init.format_slurm_topology(result)
        self.assertEqual(formatted, "topology=default:root:spine01:leaf01")

    @mock.patch("worker_init.wait_for_hostname_in_topology_config")
    def test_wait_for_topology_block_json_builds_tier_zero(
        self, mock_wait_hostname
    ):
        """A block topology update uses the node's tier-0 block."""
        node_name = "gpu-node-003"
        topology = '{"tier-0":"block1","tier-1":"leaf01","tier-2":"spine01"}'

        node_file = os.path.join(self.configmap_dir, node_name)
        with open(node_file, "w") as f:
            f.write(topology)

        config_path = Path(self.configmap_dir) / "topology.yaml"
        config_path.write_text(
            "- topology: block-nvl72\n"
            "  block:\n"
            "    blocks:\n"
            "        - block: block1\n"
            "          nodes: worker-0\n"
        )

        env = {
            "HOSTNAME": "worker-0",
            "K8S_NODE_NAME": node_name,
            "TOPOLOGY_CONFIGMAP_PATH": self.configmap_dir,
            "NODESET_GPU_ENABLED": "true",
        }
        # The plugin comes from the topology the config places this worker in, not from a
        # cluster-wide setting.
        with mock.patch.dict(os.environ, env), mock.patch(
            "worker_init.wait_for_topology_file", return_value=config_path
        ):
            result = worker_init.wait_for_topology()

        mock_wait_hostname.assert_called_once_with("worker-0", 180, 5, config_path)
        self.assertEqual(result, ("worker-0", "topology=block-nvl72:block1"))


class TestEdgeCases(unittest.TestCase):
    """Tests for edge cases and error handling."""

    def test_topology_with_special_characters(self):
        """Topology with special characters and 2 parts gets root inserted."""
        result = worker_init.format_slurm_topology("default:switch_rack-1-leaf")
        self.assertEqual(result, "topology=default:root:switch_rack-1-leaf")

    def test_topology_with_numbers(self):
        """Topology with 3+ parts is preserved (already has intermediates)."""
        result = worker_init.format_slurm_topology("default:sw001:rack42")
        self.assertEqual(result, "topology=default:sw001:rack42")

    def test_tier_with_high_numbers(self):
        """Tier format with high tier numbers builds full hierarchy sorted correctly."""
        result = worker_init.format_slurm_topology(
            "tier-1=leaf01,tier-5=fabric01,tier-10=supernet01"
        )
        # tier-10 first, tier-5 second, tier-1 (leaf) last
        self.assertEqual(result, "topology=default:root:supernet01:fabric01:leaf01")

    def test_mixed_tier_and_non_tier_keys(self):
        """Mixed tier and non-tier keys: non-tier keys are ignored."""
        result = worker_init.format_slurm_topology(
            "tier-1=leaf01,other=value,tier-2=spine01"
        )
        self.assertEqual(result, "topology=default:root:spine01:leaf01")

    def test_only_non_tier_keys(self):
        """Only non-tier keys uses first value."""
        result = worker_init.format_slurm_topology("key1=value1,key2=value2")
        # Falls back to first value
        self.assertIn("topology=default:root:", result)


class TestMainArgparse(unittest.TestCase):
    """Tests for main() argument parsing."""

    @mock.patch("worker_init.create_slurm_config_symlink")
    @mock.patch("worker_init.wait_for_controller")
    def test_main_wait_controller(self, mock_wait, mock_symlink):
        """Main calls wait_for_controller for 'wait-controller' command."""
        with mock.patch("sys.argv", ["worker_init.py", "wait-controller"]):
            worker_init.main()
        mock_symlink.assert_called_once()
        mock_wait.assert_called_once_with(create_config_symlink=False)

    @mock.patch("worker_init.apply_node_topology")
    @mock.patch("worker_init.wait_for_controller")
    @mock.patch(
        "worker_init.wait_for_topology",
        return_value=("worker-0", "topology=default:root:leaf-0"),
    )
    @mock.patch("worker_init.create_slurm_config_symlink")
    def test_main_wait_topology(
        self, mock_symlink, mock_topology, mock_controller, mock_apply
    ):
        """Topology initialization also waits for the controller before applying."""
        with mock.patch("sys.argv", ["worker_init.py", "wait-topology"]):
            worker_init.main()
        mock_symlink.assert_called_once()
        mock_topology.assert_called_once()
        mock_controller.assert_called_once_with(create_config_symlink=False)
        mock_apply.assert_called_once_with(
            "worker-0", "topology=default:root:leaf-0"
        )

    def test_main_both_commands(self):
        """Main follows topology, delay, controller, update ordering."""
        parent = mock.Mock()
        with mock.patch("worker_init.create_slurm_config_symlink") as mock_symlink, \
            mock.patch(
                "worker_init.wait_for_topology",
                return_value=("worker-0", "topology=default:root:leaf-0"),
            ) as mock_topology, \
            mock.patch("worker_init.apply_random_startup_delay") as mock_delay, \
            mock.patch("worker_init.wait_for_controller") as mock_controller, \
            mock.patch("worker_init.apply_node_topology") as mock_apply, \
            mock.patch(
                "sys.argv", ["worker_init.py", "wait-controller", "wait-topology"]
            ):
            parent.attach_mock(mock_symlink, "symlink")
            parent.attach_mock(mock_topology, "topology")
            parent.attach_mock(mock_delay, "delay")
            parent.attach_mock(mock_controller, "controller")
            parent.attach_mock(mock_apply, "apply")
            worker_init.main()

        self.assertEqual(
            parent.mock_calls,
            [
                mock.call.symlink(),
                mock.call.topology(),
                mock.call.delay(),
                mock.call.controller(create_config_symlink=False),
                mock.call.apply("worker-0", "topology=default:root:leaf-0"),
            ],
        )

    def test_main_no_command(self):
        """Main exits with error when no command is given."""
        with mock.patch("sys.argv", ["worker_init.py"]):
            with self.assertRaises(SystemExit) as ctx:
                worker_init.main()
            self.assertNotEqual(ctx.exception.code, 0)

    def test_main_invalid_command(self):
        """Main exits with error for invalid command."""
        with mock.patch("sys.argv", ["worker_init.py", "invalid"]):
            with self.assertRaises(SystemExit) as ctx:
                worker_init.main()
            self.assertNotEqual(ctx.exception.code, 0)





class TestTopologyMatchesOperatorConfig(unittest.TestCase):
    """The unit a worker registers into must be the one the operator wrote into the config.

    The operator's tree builder drops tier-0 (it names a block, not a switch) and places the node
    directly under its tier-1 switch. A worker that included tier-0 would register one switch
    deeper, on a switch the topology config never defines.
    """

    LABELS = '{"tier-0":"nvl0","tier-1":"leaf01","tier-2":"spine01"}'

    def test_tree_registration_matches_the_switch_holding_the_node(self):
        # Operator renders: SwitchName=leaf01 Nodes=<node>, under spine01, under root.
        self.assertEqual(
            worker_init.format_slurm_topology(
                self.LABELS, worker_init.TOPOLOGY_PLUGIN_TREE, "root"
            ),
            "topology=default:root:spine01:leaf01",
        )

    def test_block_registration_matches_the_block_holding_the_node(self):
        # Operator renders: BlockName=nvl0 Nodes=<node>.
        self.assertEqual(
            worker_init.format_slurm_topology(
                self.LABELS, worker_init.TOPOLOGY_PLUGIN_BLOCK, "root"
            ),
            "topology=default:nvl0",
        )



class TestParseTopologyBindings(unittest.TestCase):
    """The worker derives which topologies to join from the config it already waits for.

    That keeps it in step with the rendered file by construction: it joins exactly what the file
    places it in, with no second source of truth to drift.
    """

    CONFIG = """# Managed by Soperator.

- topology: flat
  cluster_default: true
  flat: true
- topology: tree-ib
  cluster_default: false
  tree:
    switches:
        - switch: leaf3
          nodes: h100-[0-3]
        - switch: root
          children: leaf3
- topology: block-nvl72
  cluster_default: false
  block:
    block_sizes:
        - 4
    blocks:
        - block: block7
          nodes: h100-[0-3]
"""

    def _write(self, tmp):
        path = Path(tmp) / "topology.yaml"
        path.write_text(self.CONFIG)
        return path

    def test_a_listed_worker_joins_every_topology_listing_it(self):
        with tempfile.TemporaryDirectory() as tmp:
            self.assertEqual(
                worker_init.parse_topology_bindings(self._write(tmp), "h100-2"),
                [("tree-ib", "tree"), ("block-nvl72", "block")],
            )

    def test_a_flat_topology_is_never_joined(self):
        """Flat lists no nodes and defines no unit, so there is nothing to register into."""
        with tempfile.TemporaryDirectory() as tmp:
            bindings = worker_init.parse_topology_bindings(self._write(tmp), "h100-0")
            self.assertNotIn("flat", [name for name, _ in bindings])

    def test_an_unlisted_worker_joins_nothing(self):
        with tempfile.TemporaryDirectory() as tmp:
            self.assertEqual(
                worker_init.parse_topology_bindings(self._write(tmp), "cpu-0"), []
            )

    def test_a_missing_file_is_not_fatal(self):
        self.assertEqual(
            worker_init.parse_topology_bindings(Path("/nonexistent/topology.yaml"), "h100-0"),
            [],
        )


class TestCpuOnlyWorkerInMultiTopology(unittest.TestCase):
    """A CPU-only worker appears in no node list, so it must not wait for its hostname."""

    @mock.patch("worker_init.wait_for_hostname_in_topology_config")
    @mock.patch("worker_init.is_gpu_enabled", return_value=False)
    @mock.patch(
        "worker_init.wait_for_topology_file",
        return_value=worker_init.SLURM_TOPOLOGY_YAML_PATH,
    )
    @mock.patch.dict(os.environ, {"HOSTNAME": "cpu-0"})
    def test_skips_the_hostname_wait_and_the_registration(
        self, mock_wait_file, mock_gpu, mock_wait_hostname
    ):
        result = worker_init.wait_for_topology()

        mock_wait_file.assert_called_once()
        mock_wait_hostname.assert_not_called()
        self.assertEqual(result, ("cpu-0", ""))





class TestTopologyHostnameWait(unittest.TestCase):
    """A cluster described entirely as flat names no node, so there is nothing to wait for.

    Waiting anyway would time out and crash-loop the init container of every GPU worker.
    """

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.configmap_dir = self.tmp.name

    def test_config_has_non_flat_topology(self):
        flat = Path(self.configmap_dir) / "flat.yaml"
        flat.write_text("- topology: flat\n  cluster_default: true\n  flat: true\n")
        self.assertFalse(worker_init.config_has_non_flat_topology(flat))

        tree = Path(self.configmap_dir) / "tree.yaml"
        tree.write_text(
            "- topology: tree-ib\n  tree:\n    switches:\n"
            "        - switch: leaf01\n          nodes: worker-0\n"
        )
        self.assertTrue(worker_init.config_has_non_flat_topology(tree))

        placeholder = Path(self.configmap_dir) / "placeholder.yaml"
        placeholder.write_text(
            "- topology: block-nvl72\n  block:\n    block_sizes:\n"
            "        - 1\n    blocks:\n        - block: unknown\n"
        )
        self.assertTrue(worker_init.config_has_non_flat_topology(placeholder))

    def test_a_missing_file_has_no_non_flat_topology(self):
        self.assertFalse(
            worker_init.config_has_non_flat_topology(
                Path("/nonexistent/topology.yaml")
            )
        )

    @mock.patch("worker_init.time.sleep")
    @mock.patch("worker_init.time.monotonic", side_effect=[0, 0, 1])
    def test_hostname_timeout_warns_and_continues(self, mock_monotonic, mock_sleep):
        config_path = Path(self.configmap_dir) / "topology.yaml"
        config_path.write_text("- topology: tree-ib\n  tree:\n")

        with self.assertLogs(worker_init.logger, level="WARNING") as logs:
            worker_init.wait_for_hostname_in_topology_config(
                "worker-0", 1, 1, config_path
            )

        self.assertTrue(
            any(
                "continuing without topology placement" in line
                for line in logs.output
            )
        )
        mock_sleep.assert_called_once_with(1)

    @mock.patch("worker_init.wait_for_hostname_in_topology_config")
    def test_gpu_worker_skips_the_wait_and_registers_nothing(
        self, mock_wait_hostname
    ):
        node_name = "gpu-node-001"

        config_path = Path(self.configmap_dir) / "topology.yaml"
        config_path.write_text("- topology: flat\n  cluster_default: true\n  flat: true\n")

        env = {
            "HOSTNAME": "worker-0",
            "K8S_NODE_NAME": node_name,
            "TOPOLOGY_CONFIGMAP_PATH": self.configmap_dir,
            "NODESET_GPU_ENABLED": "true",
        }
        with mock.patch.dict(os.environ, env), mock.patch(
            "worker_init.wait_for_topology_file", return_value=config_path
        ):
            result = worker_init.wait_for_topology()

        mock_wait_hostname.assert_not_called()
        self.assertEqual(result, ("worker-0", ""))

    @mock.patch("worker_init.wait_for_hostname_in_topology_config")
    def test_placeholder_waits_even_without_nodes(
        self, mock_wait_hostname
    ):
        node_name = "gpu-node-001"

        config_path = Path(self.configmap_dir) / "topology.yaml"
        config_path.write_text(
            "- topology: block-nvl72\n  block:\n    block_sizes:\n"
            "        - 1\n    blocks:\n        - block: unknown\n"
        )

        env = {
            "HOSTNAME": "worker-0",
            "K8S_NODE_NAME": node_name,
            "TOPOLOGY_CONFIGMAP_PATH": self.configmap_dir,
            "NODESET_GPU_ENABLED": "true",
        }
        with mock.patch.dict(os.environ, env), mock.patch(
            "worker_init.wait_for_topology_file", return_value=config_path
        ):
            result = worker_init.wait_for_topology()

        mock_wait_hostname.assert_called_once_with("worker-0", 180, 5, config_path)
        self.assertEqual(result, ("worker-0", ""))


if __name__ == "__main__":
    unittest.main(verbosity=2)
