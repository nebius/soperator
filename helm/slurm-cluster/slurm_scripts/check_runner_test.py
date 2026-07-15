import importlib.util
import os
import tempfile
import unittest
from pathlib import Path
from unittest import mock


MODULE_PATH = Path(__file__).with_name("check_runner.py")


def load_check_runner():
    required_env = {
        "SLURMD_NODENAME": "worker-1",
        "CHECKS_OUTPUTS_BASE_DIR": "/opt/soperator-outputs",
        "CHECKS_CONTEXT": "hc_program",
        "CHECKS_CONFIG": "/opt/slurm_scripts/checks.json",
    }
    with mock.patch.dict(os.environ, required_env):
        spec = importlib.util.spec_from_file_location(
            "check_runner_under_test", MODULE_PATH
        )
        module = importlib.util.module_from_spec(spec)
        assert spec.loader is not None
        spec.loader.exec_module(module)
        return module


check_runner = load_check_runner()


class NodeRealMemoryMetadataTest(unittest.TestCase):
    def setUp(self):
        check_runner.get_node_real_memory_bytes.cache_clear()

    def test_reads_real_memory_from_local_metadata_without_node_rpc(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            metadata_file = Path(tmpdir) / "node_metadata.env"
            metadata_file.write_text(
                "IGNORED=value\n"
                "SOPERATOR_NODE_REAL_MEMORY_BYTES=999292928\n",
                encoding="utf-8",
            )

            with (
                mock.patch.object(
                    check_runner, "SOPERATOR_NODE_METADATA_FILE", str(metadata_file)
                ),
                mock.patch.object(check_runner, "get_node_info") as get_node_info,
            ):
                result = check_runner.get_node_real_memory_bytes()

            self.assertEqual(999292928, result)
            get_node_info.assert_not_called()

    def test_exports_local_real_memory_for_checks(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            metadata_file = Path(tmpdir) / "node_metadata.env"
            metadata_file.write_text(
                "SOPERATOR_NODE_REAL_MEMORY_BYTES=999292928\n",
                encoding="utf-8",
            )
            check = check_runner.Check(need_env=["CHECKS_NODE_REAL_MEM_BYTES"])

            with (
                mock.patch.object(
                    check_runner, "SOPERATOR_NODE_METADATA_FILE", str(metadata_file)
                ),
                mock.patch.object(check_runner, "get_node_info") as get_node_info,
                mock.patch.dict(os.environ, {}, clear=False),
            ):
                check_runner.export_needed_env(check)
                exported_value = os.environ["CHECKS_NODE_REAL_MEM_BYTES"]

            self.assertEqual("999292928", exported_value)
            get_node_info.assert_not_called()

    def test_falls_back_to_slurm_when_metadata_is_missing(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            missing_file = Path(tmpdir) / "missing.env"
            node_info = check_runner.NodeInfo(real_memory_bytes=2147483648)

            with (
                mock.patch.object(
                    check_runner, "SOPERATOR_NODE_METADATA_FILE", str(missing_file)
                ),
                mock.patch.object(
                    check_runner, "get_node_info", return_value=node_info
                ) as get_node_info,
            ):
                result = check_runner.get_node_real_memory_bytes()

            self.assertEqual(2147483648, result)
            get_node_info.assert_called_once_with()

    def test_falls_back_to_slurm_when_metadata_is_invalid(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            metadata_file = Path(tmpdir) / "node_metadata.env"
            metadata_file.write_text(
                "SOPERATOR_NODE_REAL_MEMORY_BYTES=invalid\n", encoding="utf-8"
            )
            node_info = check_runner.NodeInfo(real_memory_bytes=1073741824)

            with (
                mock.patch.object(
                    check_runner, "SOPERATOR_NODE_METADATA_FILE", str(metadata_file)
                ),
                mock.patch.object(
                    check_runner, "get_node_info", return_value=node_info
                ) as get_node_info,
            ):
                result = check_runner.get_node_real_memory_bytes()

            self.assertEqual(1073741824, result)
            get_node_info.assert_called_once_with()


if __name__ == "__main__":
    unittest.main(verbosity=2)
