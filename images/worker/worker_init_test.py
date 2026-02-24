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

    def test_tier_format_single_tier(self):
        """Single tier format is converted correctly."""
        result = worker_init.format_slurm_topology("tier-0=switch1")
        self.assertEqual(result, "topology=default:root:switch1")

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
        result = worker_init.format_slurm_topology(
            "tier-1 = leaf01 , tier-2 = spine01"
        )
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
        self.assertEqual(result, "topology=default:root:5df641bb92d51e0dd5d97037fc7e2971:4dcbe855beb5ce19f484ba1a8960929d")

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

    def test_json_format_with_tier_zero(self):
        """JSON format with tier-0 builds full hierarchy including tier-0 as leaf."""
        result = worker_init.format_slurm_topology(
            '{"tier-0":"nvl0","tier-1":"leaf01"}'
        )
        # tier-1 (rack/leaf-switch) first, tier-0 (NVL domain) last
        self.assertEqual(result, "topology=default:root:leaf01:nvl0")


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
        result = worker_init._format_tier_topology({
            "tier-1": "leaf01",
            "tier-2": "spine01"
        })
        self.assertEqual(result, "topology=default:root:spine01:leaf01")

    def test_tier_zero_included_as_leaf(self):
        """tier-0 is included as the innermost switch (leaf/block domain)."""
        result = worker_init._format_tier_topology({
            "tier-0": "nvl0",
            "tier-1": "leaf01",
            "tier-2": "spine01"
        })
        self.assertEqual(result, "topology=default:root:spine01:leaf01:nvl0")

    def test_three_tiers_builds_hierarchy(self):
        """Three tiers builds full path from fabric to leaf."""
        result = worker_init._format_tier_topology({
            "tier-1": "leaf01",
            "tier-2": "spine01",
            "tier-3": "fabric01"
        })
        self.assertEqual(result, "topology=default:root:fabric01:spine01:leaf01")

    def test_unordered_tier_keys(self):
        """Tier keys in any order produce the same sorted hierarchy."""
        result = worker_init._format_tier_topology({
            "tier-3": "fabric01",
            "tier-1": "leaf01",
            "tier-2": "spine01"
        })
        self.assertEqual(result, "topology=default:root:fabric01:spine01:leaf01")

    def test_non_tier_keys_ignored(self):
        """Non-tier keys are ignored, tier keys used."""
        result = worker_init._format_tier_topology({
            "other": "value",
            "tier-1": "leaf01",
            "name": "test"
        })
        self.assertEqual(result, "topology=default:root:leaf01")

    def test_only_non_tier_keys_uses_first_value(self):
        """Only non-tier keys uses first value as fallback."""
        result = worker_init._format_tier_topology({
            "switch": "sw1",
            "rack": "r1"
        })
        self.assertEqual(result, "topology=default:root:sw1")

    def test_invalid_tier_format_ignored(self):
        """Invalid tier format keys are ignored."""
        result = worker_init._format_tier_topology({
            "tier-abc": "invalid",
            "tier-1": "leaf01"
        })
        self.assertEqual(result, "topology=default:root:leaf01")

    def test_tier_with_hash_value(self):
        """Tier with hash value (real ConfigMap data) builds full hierarchy."""
        result = worker_init._format_tier_topology({
            "tier-1": "4dcbe855beb5ce19f484ba1a8960929d",
            "tier-2": "5df641bb92d51e0dd5d97037fc7e2971"
        })
        # tier-2 (spine) first, tier-1 (leaf) last
        self.assertEqual(result, "topology=default:root:5df641bb92d51e0dd5d97037fc7e2971:4dcbe855beb5ce19f484ba1a8960929d")


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

    def test_get_topology_path_default(self):
        """Get topology path returns default when not set."""
        env = os.environ.copy()
        env.pop("TOPOLOGY_CONFIGMAP_PATH", None)
        with mock.patch.dict(os.environ, env, clear=True):
            result = worker_init.get_topology_path()
        self.assertEqual(result, "/tmp/slurm/topology-node-labels")

    def test_get_topology_path_custom(self):
        """Get topology path returns custom value when set."""
        with mock.patch.dict(os.environ, {"TOPOLOGY_CONFIGMAP_PATH": "/custom/path"}):
            result = worker_init.get_topology_path()
        self.assertEqual(result, "/custom/path")

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
        with mock.patch.object(worker_init, "SLURM_CONFIG_LINK_SOURCE", self.source), \
             mock.patch.object(worker_init, "SLURM_CONFIG_LINK_TARGET", self.target):
            worker_init.create_slurm_config_symlink()

        self.assertTrue(os.path.islink(self.target))
        self.assertEqual(os.readlink(self.target), self.source)

    def test_create_symlink_replaces_existing_symlink(self):
        """Replaces existing symlink at target."""
        os.symlink("/some/old/path", self.target)

        with mock.patch.object(worker_init, "SLURM_CONFIG_LINK_SOURCE", self.source), \
             mock.patch.object(worker_init, "SLURM_CONFIG_LINK_TARGET", self.target):
            worker_init.create_slurm_config_symlink()

        self.assertTrue(os.path.islink(self.target))
        self.assertEqual(os.readlink(self.target), self.source)

    def test_create_symlink_replaces_existing_directory(self):
        """Replaces existing directory at target."""
        os.makedirs(self.target)
        # Create a file inside to ensure rmtree works
        with open(os.path.join(self.target, "test.conf"), "w") as f:
            f.write("test")

        with mock.patch.object(worker_init, "SLURM_CONFIG_LINK_SOURCE", self.source), \
             mock.patch.object(worker_init, "SLURM_CONFIG_LINK_TARGET", self.target):
            worker_init.create_slurm_config_symlink()

        self.assertTrue(os.path.islink(self.target))
        self.assertEqual(os.readlink(self.target), self.source)


class TestWaitForController(unittest.TestCase):
    """Tests for wait_for_controller function."""

    _PING_UP_JSON = json.dumps({
        "pings": [{"hostname": "controller-0", "pinged": "UP", "responding": True, "mode": "primary"}],
        "errors": [], "warnings": [],
    })

    _PING_DOWN_JSON = json.dumps({
        "pings": [{"hostname": "controller-0", "pinged": "DOWN", "responding": False, "mode": "primary"}],
        "errors": [], "warnings": [],
    })

    @mock.patch("worker_init.create_slurm_config_symlink")
    @mock.patch("subprocess.run")
    def test_controller_ready_immediately(self, mock_run, mock_symlink):
        """Controller is ready on first attempt."""
        mock_run.return_value = mock.Mock(returncode=0, stdout=self._PING_UP_JSON, stderr="")

        with mock.patch.dict(os.environ, {"CONTROLLER_MAX_ATTEMPTS": "5", "CONTROLLER_POLL_INTERVAL": "0"}):
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

        with mock.patch.dict(os.environ, {"CONTROLLER_MAX_ATTEMPTS": "5", "CONTROLLER_POLL_INTERVAL": "1"}):
            worker_init.wait_for_controller()

        self.assertEqual(mock_run.call_count, 3)
        self.assertEqual(mock_sleep.call_count, 2)

    @mock.patch("worker_init.create_slurm_config_symlink")
    @mock.patch("subprocess.run")
    @mock.patch("time.sleep")
    def test_controller_not_responding_retries(self, mock_sleep, mock_run, mock_symlink):
        """Controller returns JSON but responding=false, retries until ready."""
        mock_run.side_effect = [
            mock.Mock(returncode=0, stdout=self._PING_DOWN_JSON, stderr=""),
            mock.Mock(returncode=0, stdout=self._PING_UP_JSON, stderr=""),
        ]

        with mock.patch.dict(os.environ, {"CONTROLLER_MAX_ATTEMPTS": "5", "CONTROLLER_POLL_INTERVAL": "0"}):
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

        with mock.patch.dict(os.environ, {"CONTROLLER_MAX_ATTEMPTS": "5", "CONTROLLER_POLL_INTERVAL": "0"}):
            worker_init.wait_for_controller()

        self.assertEqual(mock_run.call_count, 2)

    @mock.patch("worker_init.create_slurm_config_symlink")
    @mock.patch("subprocess.run")
    @mock.patch("time.sleep")
    def test_controller_timeout(self, mock_sleep, mock_run, mock_symlink):
        """Controller does not become ready within max attempts."""
        mock_run.return_value = mock.Mock(returncode=1, stdout="", stderr="connection refused")

        with mock.patch.dict(os.environ, {"CONTROLLER_MAX_ATTEMPTS": "3", "CONTROLLER_POLL_INTERVAL": "0"}):
            with self.assertRaises(SystemExit) as ctx:
                worker_init.wait_for_controller()
            self.assertEqual(ctx.exception.code, 1)

        self.assertEqual(mock_run.call_count, 3)

    @mock.patch("worker_init.create_slurm_config_symlink")
    @mock.patch("subprocess.run")
    def test_controller_scontrol_not_found(self, mock_run, mock_symlink):
        """scontrol command not found exits immediately."""
        mock_run.side_effect = FileNotFoundError("scontrol not found")

        with mock.patch.dict(os.environ, {"CONTROLLER_MAX_ATTEMPTS": "5", "CONTROLLER_POLL_INTERVAL": "0"}):
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

        with mock.patch.dict(os.environ, {"CONTROLLER_MAX_ATTEMPTS": "5", "CONTROLLER_POLL_INTERVAL": "0"}):
            worker_init.wait_for_controller()

        self.assertEqual(mock_run.call_count, 2)

    @mock.patch("worker_init.create_slurm_config_symlink")
    @mock.patch("subprocess.run")
    @mock.patch("time.sleep")
    def test_controller_multiple_pings_all_must_be_up(self, mock_sleep, mock_run, mock_symlink):
        """All controllers in pings array must be UP and responding."""
        partial_json = json.dumps({
            "pings": [
                {"hostname": "ctrl-0", "pinged": "UP", "responding": True},
                {"hostname": "ctrl-1", "pinged": "DOWN", "responding": False},
            ],
            "errors": [], "warnings": [],
        })
        all_up_json = json.dumps({
            "pings": [
                {"hostname": "ctrl-0", "pinged": "UP", "responding": True},
                {"hostname": "ctrl-1", "pinged": "UP", "responding": True},
            ],
            "errors": [], "warnings": [],
        })
        mock_run.side_effect = [
            mock.Mock(returncode=0, stdout=partial_json, stderr=""),
            mock.Mock(returncode=0, stdout=all_up_json, stderr=""),
        ]

        with mock.patch.dict(os.environ, {"CONTROLLER_MAX_ATTEMPTS": "5", "CONTROLLER_POLL_INTERVAL": "0"}):
            worker_init.wait_for_controller()

        self.assertEqual(mock_run.call_count, 2)


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


class TestEdgeCases(unittest.TestCase):
    """Tests for edge cases and error handling."""

    def test_topology_with_special_characters(self):
        """Topology with special characters and 2 parts gets root inserted."""
        result = worker_init.format_slurm_topology("default:switch_rack-1.leaf")
        self.assertEqual(result, "topology=default:root:switch_rack-1.leaf")

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

    @mock.patch("worker_init.wait_for_controller")
    def test_main_wait_controller(self, mock_wait):
        """Main calls wait_for_controller for 'wait-controller' command."""
        with mock.patch("sys.argv", ["worker_init.py", "wait-controller"]):
            worker_init.main()
        mock_wait.assert_called_once()

    @mock.patch("worker_init.wait_for_topology")
    def test_main_wait_topology(self, mock_wait):
        """Main calls wait_for_topology for 'wait-topology' command."""
        with mock.patch("sys.argv", ["worker_init.py", "wait-topology"]):
            worker_init.main()
        mock_wait.assert_called_once()

    @mock.patch("worker_init.wait_for_topology")
    @mock.patch("worker_init.wait_for_controller")
    def test_main_both_commands(self, mock_controller, mock_topology):
        """Main runs both commands sequentially."""
        with mock.patch("sys.argv", ["worker_init.py", "wait-controller", "wait-topology"]):
            worker_init.main()
        mock_controller.assert_called_once()
        mock_topology.assert_called_once()

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


if __name__ == "__main__":
    unittest.main(verbosity=2)
