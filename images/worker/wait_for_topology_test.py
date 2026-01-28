#!/usr/bin/env python3
"""
Unit tests for wait_for_topology.py

Run with: python3 -m pytest wait_for_topology_test.py -v
Or without pytest: python3 wait_for_topology_test.py
"""

import os
import tempfile
import unittest
import unittest.mock as mock

# Import the module under test
import wait_for_topology


class TestFormatSlurmTopology(unittest.TestCase):
    """Tests for format_slurm_topology function."""

    def test_empty_topology(self):
        """Empty string returns empty string."""
        result = wait_for_topology.format_slurm_topology("")
        self.assertEqual(result, "")

    def test_none_topology(self):
        """None returns empty string."""
        result = wait_for_topology.format_slurm_topology(None)
        self.assertEqual(result, "")

    def test_simple_switch_name(self):
        """Simple switch name is formatted with default topology."""
        result = wait_for_topology.format_slurm_topology("switch1")
        self.assertEqual(result, "Topology=default:switch1")

    def test_topology_with_name_and_switch(self):
        """Format 'name:switch' is preserved."""
        result = wait_for_topology.format_slurm_topology("default:switch1")
        self.assertEqual(result, "Topology=default:switch1")

    def test_topology_with_intermediate_switches(self):
        """Format 'name:sw1:sw2:sw3' is preserved."""
        result = wait_for_topology.format_slurm_topology("default:sw_root:s1:s2")
        self.assertEqual(result, "Topology=default:sw_root:s1:s2")

    def test_custom_topology_name(self):
        """Custom topology name is preserved."""
        result = wait_for_topology.format_slurm_topology("my-topo:leaf-switch")
        self.assertEqual(result, "Topology=my-topo:leaf-switch")

    def test_tier_format_single_tier(self):
        """Single tier format is converted correctly."""
        result = wait_for_topology.format_slurm_topology("tier-0=switch1")
        self.assertEqual(result, "Topology=default:switch1")

    def test_tier_format_two_tiers(self):
        """Two tier format uses lowest tier as leaf switch."""
        result = wait_for_topology.format_slurm_topology("tier-0=leaf,tier-1=root")
        # tier-0 (leaf) is the lowest, used for dynamic topology
        self.assertEqual(result, "Topology=default:leaf")

    def test_tier_format_three_tiers(self):
        """Three tier format uses lowest tier as leaf switch."""
        result = wait_for_topology.format_slurm_topology(
            "tier-0=leaf,tier-1=mid,tier-2=root"
        )
        # tier-0 (leaf) is the lowest
        self.assertEqual(result, "Topology=default:leaf")

    def test_tier_format_with_spaces(self):
        """Tier format with spaces is handled correctly."""
        result = wait_for_topology.format_slurm_topology(
            "tier-0 = switch1 , tier-1 = rack1"
        )
        self.assertEqual(result, "Topology=default:switch1")

    def test_tier_format_unordered(self):
        """Tier format with unordered tiers uses lowest tier."""
        result = wait_for_topology.format_slurm_topology(
            "tier-2=top,tier-0=bottom,tier-1=middle"
        )
        self.assertEqual(result, "Topology=default:bottom")

    def test_json_format_two_tiers(self):
        """JSON format with two tiers uses lowest tier as leaf."""
        result = wait_for_topology.format_slurm_topology(
            '{"tier-1":"4dcbe855beb5ce19f484ba1a8960929d","tier-2":"5df641bb92d51e0dd5d97037fc7e2971"}'
        )
        # tier-1 is the lowest tier here, used as leaf switch
        self.assertEqual(result, "Topology=default:4dcbe855beb5ce19f484ba1a8960929d")

    def test_json_format_single_tier(self):
        """JSON format with single tier is parsed correctly."""
        result = wait_for_topology.format_slurm_topology('{"tier-1":"switch1"}')
        self.assertEqual(result, "Topology=default:switch1")

    def test_json_format_three_tiers(self):
        """JSON format with three tiers uses lowest tier."""
        result = wait_for_topology.format_slurm_topology(
            '{"tier-1":"leaf","tier-2":"mid","tier-3":"root"}'
        )
        # tier-1 is the lowest
        self.assertEqual(result, "Topology=default:leaf")

    def test_json_format_with_whitespace(self):
        """JSON format with whitespace is handled correctly."""
        result = wait_for_topology.format_slurm_topology(
            '  {"tier-1": "switch1", "tier-2": "rack1"}  '
        )
        self.assertEqual(result, "Topology=default:switch1")

    def test_json_format_with_tier_zero(self):
        """JSON format with tier-0 uses it as leaf (block topology)."""
        result = wait_for_topology.format_slurm_topology(
            '{"tier-0":"block1","tier-1":"rack1"}'
        )
        # tier-0 is the lowest, used for block topology
        self.assertEqual(result, "Topology=default:block1")


class TestFormatTierTopology(unittest.TestCase):
    """Tests for _format_tier_topology internal function."""

    def test_empty_dict(self):
        """Empty dictionary returns empty string."""
        result = wait_for_topology._format_tier_topology({})
        self.assertEqual(result, "")

    def test_none_input(self):
        """None input returns empty string."""
        result = wait_for_topology._format_tier_topology(None)
        self.assertEqual(result, "")

    def test_single_tier(self):
        """Single tier returns that tier value."""
        result = wait_for_topology._format_tier_topology({"tier-1": "switch1"})
        self.assertEqual(result, "Topology=default:switch1")

    def test_two_tiers_uses_lowest(self):
        """Two tiers uses the lowest tier number."""
        result = wait_for_topology._format_tier_topology({
            "tier-1": "leaf",
            "tier-2": "spine"
        })
        self.assertEqual(result, "Topology=default:leaf")

    def test_tier_zero_is_lowest(self):
        """tier-0 is considered lowest (block topology)."""
        result = wait_for_topology._format_tier_topology({
            "tier-0": "block1",
            "tier-1": "rack1",
            "tier-2": "spine1"
        })
        self.assertEqual(result, "Topology=default:block1")

    def test_three_tiers_uses_lowest(self):
        """Three tiers with tier-1 as lowest."""
        result = wait_for_topology._format_tier_topology({
            "tier-1": "leaf00",
            "tier-2": "spine00",
            "tier-3": "superspine"
        })
        self.assertEqual(result, "Topology=default:leaf00")

    def test_unordered_tier_keys(self):
        """Tier keys in any order still finds lowest."""
        result = wait_for_topology._format_tier_topology({
            "tier-3": "top",
            "tier-1": "bottom",
            "tier-2": "middle"
        })
        self.assertEqual(result, "Topology=default:bottom")

    def test_non_tier_keys_ignored(self):
        """Non-tier keys are ignored, tier keys used."""
        result = wait_for_topology._format_tier_topology({
            "other": "value",
            "tier-1": "switch1",
            "name": "test"
        })
        self.assertEqual(result, "Topology=default:switch1")

    def test_only_non_tier_keys_uses_first_value(self):
        """Only non-tier keys uses first value as fallback."""
        result = wait_for_topology._format_tier_topology({
            "switch": "sw1",
            "rack": "r1"
        })
        self.assertEqual(result, "Topology=default:sw1")

    def test_invalid_tier_format_ignored(self):
        """Invalid tier format keys are ignored."""
        result = wait_for_topology._format_tier_topology({
            "tier-abc": "invalid",
            "tier-1": "valid"
        })
        self.assertEqual(result, "Topology=default:valid")

    def test_tier_with_hash_value(self):
        """Tier with hash value (real ConfigMap data)."""
        result = wait_for_topology._format_tier_topology({
            "tier-1": "4dcbe855beb5ce19f484ba1a8960929d",
            "tier-2": "5df641bb92d51e0dd5d97037fc7e2971"
        })
        self.assertEqual(result, "Topology=default:4dcbe855beb5ce19f484ba1a8960929d")


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
        
        result = wait_for_topology.read_topology_for_node(self.temp_dir, node_name)
        self.assertEqual(result, topology)

    def test_read_nonexistent_node(self):
        """Reading topology for non-existent node returns None."""
        result = wait_for_topology.read_topology_for_node(self.temp_dir, "nonexistent")
        self.assertIsNone(result)

    def test_read_empty_file(self):
        """Reading empty file returns None."""
        node_name = "empty-node"
        node_file = os.path.join(self.temp_dir, node_name)
        with open(node_file, "w") as f:
            f.write("")
        
        result = wait_for_topology.read_topology_for_node(self.temp_dir, node_name)
        self.assertIsNone(result)

    def test_read_whitespace_only(self):
        """Reading whitespace-only file returns None."""
        node_name = "whitespace-node"
        node_file = os.path.join(self.temp_dir, node_name)
        with open(node_file, "w") as f:
            f.write("   \n\t  \n")
        
        result = wait_for_topology.read_topology_for_node(self.temp_dir, node_name)
        self.assertIsNone(result)

    def test_read_strips_whitespace(self):
        """Reading file strips leading/trailing whitespace."""
        node_name = "node-with-whitespace"
        node_file = os.path.join(self.temp_dir, node_name)
        with open(node_file, "w") as f:
            f.write("  default:switch1  \n")
        
        result = wait_for_topology.read_topology_for_node(self.temp_dir, node_name)
        self.assertEqual(result, "default:switch1")


class TestWriteTopologyEnv(unittest.TestCase):
    """Tests for write_topology_env function."""

    def setUp(self):
        """Create a temporary directory for test files."""
        self.temp_dir = tempfile.mkdtemp()
        self.env_file = os.path.join(self.temp_dir, "slurm_topology.env")

    def tearDown(self):
        """Clean up temporary directory."""
        import shutil
        shutil.rmtree(self.temp_dir)

    def test_write_simple_topology(self):
        """Write simple topology to env file."""
        with mock.patch.dict(os.environ, {"TOPOLOGY_ENV_FILE": self.env_file}):
            result = wait_for_topology.write_topology_env("default:switch1")
        
        self.assertTrue(result)
        with open(self.env_file, "r") as f:
            content = f.read()
        self.assertEqual(content, 'SLURM_NODE_TOPOLOGY="Topology=default:switch1"\n')

    def test_write_tier_topology(self):
        """Write tier-based topology to env file."""
        with mock.patch.dict(os.environ, {"TOPOLOGY_ENV_FILE": self.env_file}):
            result = wait_for_topology.write_topology_env("tier-0=leaf,tier-1=root")
        
        self.assertTrue(result)
        with open(self.env_file, "r") as f:
            content = f.read()
        # Uses lowest tier (tier-0) as leaf switch
        self.assertEqual(content, 'SLURM_NODE_TOPOLOGY="Topology=default:leaf"\n')

    def test_write_to_invalid_path(self):
        """Writing to invalid path returns False."""
        invalid_path = "/nonexistent/directory/file.env"
        with mock.patch.dict(os.environ, {"TOPOLOGY_ENV_FILE": invalid_path}):
            result = wait_for_topology.write_topology_env("default:switch1")
        
        self.assertFalse(result)


class TestGetEnvironmentVariables(unittest.TestCase):
    """Tests for environment variable getter functions."""

    def test_get_node_name_set(self):
        """Get node name when environment variable is set."""
        with mock.patch.dict(os.environ, {"K8S_NODE_NAME": "test-node-001"}):
            result = wait_for_topology.get_node_name()
        self.assertEqual(result, "test-node-001")

    def test_get_node_name_not_set(self):
        """Get node name when environment variable is not set raises KeyError."""
        env = os.environ.copy()
        env.pop("K8S_NODE_NAME", None)
        with mock.patch.dict(os.environ, env, clear=True):
            with self.assertRaises(KeyError):
                wait_for_topology.get_node_name()

    def test_get_topology_path_default(self):
        """Get topology path returns default when not set."""
        env = os.environ.copy()
        env.pop("TOPOLOGY_CONFIGMAP_PATH", None)
        with mock.patch.dict(os.environ, env, clear=True):
            result = wait_for_topology.get_topology_path()
        self.assertEqual(result, "/etc/slurm/topology-node-labels")

    def test_get_topology_path_custom(self):
        """Get topology path returns custom value when set."""
        with mock.patch.dict(os.environ, {"TOPOLOGY_CONFIGMAP_PATH": "/custom/path"}):
            result = wait_for_topology.get_topology_path()
        self.assertEqual(result, "/custom/path")

    def test_get_wait_timeout_default(self):
        """Get wait timeout returns default when not set."""
        env = os.environ.copy()
        env.pop("TOPOLOGY_WAIT_TIMEOUT", None)
        with mock.patch.dict(os.environ, env, clear=True):
            result = wait_for_topology.get_wait_timeout()
        self.assertEqual(result, 180)

    def test_get_wait_timeout_custom(self):
        """Get wait timeout returns custom value when set."""
        with mock.patch.dict(os.environ, {"TOPOLOGY_WAIT_TIMEOUT": "300"}):
            result = wait_for_topology.get_wait_timeout()
        self.assertEqual(result, 300)

    def test_get_wait_timeout_invalid(self):
        """Get wait timeout raises ValueError for invalid value."""
        with mock.patch.dict(os.environ, {"TOPOLOGY_WAIT_TIMEOUT": "invalid"}):
            with self.assertRaises(ValueError):
                wait_for_topology.get_wait_timeout()

    def test_get_poll_interval_default(self):
        """Get poll interval returns default when not set."""
        env = os.environ.copy()
        env.pop("TOPOLOGY_POLL_INTERVAL", None)
        with mock.patch.dict(os.environ, env, clear=True):
            result = wait_for_topology.get_poll_interval()
        self.assertEqual(result, 5)

    def test_get_poll_interval_custom(self):
        """Get poll interval returns custom value when set."""
        with mock.patch.dict(os.environ, {"TOPOLOGY_POLL_INTERVAL": "10"}):
            result = wait_for_topology.get_poll_interval()
        self.assertEqual(result, 10)


class TestIntegration(unittest.TestCase):
    """Integration tests for the full flow."""

    def setUp(self):
        """Create temporary directories for test files."""
        self.temp_dir = tempfile.mkdtemp()
        self.configmap_dir = os.path.join(self.temp_dir, "configmap")
        self.env_file = os.path.join(self.temp_dir, "slurm_topology.env")
        os.makedirs(self.configmap_dir)

    def tearDown(self):
        """Clean up temporary directories."""
        import shutil
        shutil.rmtree(self.temp_dir)

    def test_full_flow_node_exists(self):
        """Test full flow when node topology exists."""
        node_name = "gpu-node-001"
        topology = "tier-0=switch-rack1,tier-1=spine1"
        
        # Create node topology file
        node_file = os.path.join(self.configmap_dir, node_name)
        with open(node_file, "w") as f:
            f.write(topology)
        
        # Read topology
        result = wait_for_topology.read_topology_for_node(self.configmap_dir, node_name)
        self.assertEqual(result, topology)
        
        # Write env file
        with mock.patch.dict(os.environ, {"TOPOLOGY_ENV_FILE": self.env_file}):
            success = wait_for_topology.write_topology_env(result)
        self.assertTrue(success)
        
        # Verify env file content - uses lowest tier (tier-0) as leaf switch
        with open(self.env_file, "r") as f:
            content = f.read()
        self.assertEqual(content, 'SLURM_NODE_TOPOLOGY="Topology=default:switch-rack1"\n')

    def test_env_file_can_be_sourced(self):
        """Test that the generated env file can be sourced by bash."""
        node_name = "test-node"
        topology = "default:switch1"
        
        # Create node topology file
        node_file = os.path.join(self.configmap_dir, node_name)
        with open(node_file, "w") as f:
            f.write(topology)
        
        # Write env file
        with mock.patch.dict(os.environ, {"TOPOLOGY_ENV_FILE": self.env_file}):
            wait_for_topology.write_topology_env(topology)
        
        # Verify the file can be sourced and variable extracted
        import subprocess
        result = subprocess.run(
            ["bash", "-c", f". {self.env_file} && echo $SLURM_NODE_TOPOLOGY"],
            capture_output=True,
            text=True
        )
        self.assertEqual(result.returncode, 0)
        self.assertEqual(result.stdout.strip(), "Topology=default:switch1")


class TestEdgeCases(unittest.TestCase):
    """Tests for edge cases and error handling."""

    def test_topology_with_special_characters(self):
        """Topology with special characters is handled."""
        result = wait_for_topology.format_slurm_topology("default:switch_rack-1.leaf")
        self.assertEqual(result, "Topology=default:switch_rack-1.leaf")

    def test_topology_with_numbers(self):
        """Topology with numbers is handled."""
        result = wait_for_topology.format_slurm_topology("default:sw001:rack42")
        self.assertEqual(result, "Topology=default:sw001:rack42")

    def test_tier_with_high_numbers(self):
        """Tier format with high tier numbers uses lowest tier."""
        result = wait_for_topology.format_slurm_topology(
            "tier-0=l0,tier-5=l5,tier-10=l10"
        )
        # Should use tier-0 as the lowest tier
        self.assertEqual(result, "Topology=default:l0")

    def test_mixed_tier_and_non_tier_keys(self):
        """Mixed tier and non-tier keys uses lowest tier key."""
        result = wait_for_topology.format_slurm_topology(
            "tier-0=switch1,other=value,tier-1=rack1"
        )
        self.assertEqual(result, "Topology=default:switch1")

    def test_only_non_tier_keys(self):
        """Only non-tier keys uses first value."""
        result = wait_for_topology.format_slurm_topology("key1=value1,key2=value2")
        # Falls back to first value
        self.assertIn("Topology=default:", result)


if __name__ == "__main__":
    unittest.main(verbosity=2)
