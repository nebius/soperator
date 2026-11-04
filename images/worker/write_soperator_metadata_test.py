import os
import stat
import subprocess
import tempfile
import unittest
from pathlib import Path


SCRIPT_PATH = Path(__file__).with_name("write_soperator_metadata.sh")


class WriteSoperatorMetadataTest(unittest.TestCase):
    def run_writer(self, metadata_file: Path, **metadata_env: str) -> subprocess.CompletedProcess[str]:
        return subprocess.run(
            ["bash", str(SCRIPT_PATH), str(metadata_file)],
            check=False,
            env={"PATH": os.environ["PATH"], **metadata_env},
            capture_output=True,
            text=True,
        )

    def test_writes_prepared_metadata_atomically(self):
        for tag, tags, enabled in (("CPU", "CPU", "false"), ("8xH200", "8xH200,8xGPU", "true")):
            with self.subTest(tag=tag), tempfile.TemporaryDirectory() as tmpdir:
                metadata_file = Path(tmpdir) / "nested" / "node_metadata.env"
                env = {
                    "NODESET_GPU_ENABLED": enabled,
                    "SOPERATOR_NODE_REAL_MEMORY_BYTES": "999292928",
                    "SOPERATOR_NODE_PLATFORM_TAG": tag,
                    "SOPERATOR_NODE_PLATFORM_TAGS": tags,
                    "SOPERATOR_NODE_CPUS": "128",
                    "SOPERATOR_NODE_BOARDS": "1",
                    "SOPERATOR_NODE_SOCKETSPERBOARD": "2",
                    "SOPERATOR_NODE_CORESPERSOCKET": "32",
                    "SOPERATOR_NODE_THREADSPERCORE": "2",
                    "SOPERATOR_NODE_GRES": "gpu:nvidia_h200:8",
                    "CUSTOM_SETTING": "value with spaces=and=equals",
                    "EMPTY_SETTING": "",
                }
                result = self.run_writer(metadata_file, **env)
                self.assertEqual(0, result.returncode, result.stderr)
                self.assertEqual(env, dict(line.split("=", 1) for line in metadata_file.read_text().splitlines()))
                self.assertEqual(0o644, stat.S_IMODE(metadata_file.stat().st_mode))
                self.assertEqual([], list(metadata_file.parent.glob("*.tmp.*")))

    def test_filters_environment_by_requested_prefixes(self):
        excluded = {
            "KUBERNETES_SERVICE_HOST": "kubernetes", "HELM_TEST": "helm", "LC_TEST": "locale",
            "LS_COLORS": "colors", "TZ": "UTC", "LESS": "less", "LESSOPEN": "lessopen",
            "DEBIAN_FRONTEND": "noninteractive", "SHLVL": "2", "TERM": "xterm", "LANG": "C",
            "HOME": "/tmp/home", "PWD": "/tmp/work", "_": "last-command",
            "NVIDIA_VISIBLE_DEVICES": "all", "LD_LIBRARY_PATH": "/usr/lib", "LD_PRELOAD": "lib.so",
            "MULTI_LINE": "first\nsecond=value", "CARRIAGE_RETURN": "first\rsecond", "my.var": "dot", "foo-bar": "dash",
        }
        retained = {"CUSTOM": "test", "TZ_NAME": "UTC", "LANGUAGE": "C", "HOME_DIR": "/tmp", "NVIDIA": "x", "LDAP_URI": "ldap://"}
        with tempfile.TemporaryDirectory() as tmpdir:
            metadata_file = Path(tmpdir) / "node_metadata.env"
            result = self.run_writer(metadata_file, **excluded, **retained)
            self.assertEqual(0, result.returncode, result.stderr)
            self.assertEqual(retained, dict(line.split("=", 1) for line in metadata_file.read_text().splitlines()))

    def test_replaces_existing_metadata_without_stale_variables(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            metadata_file = Path(tmpdir) / "node_metadata.env"
            metadata_file.write_text("STALE_KEY=old\nSOPERATOR_NODE_PLATFORM_TAG=8xH200\n")
            result = self.run_writer(metadata_file, SOPERATOR_NODE_PLATFORM_TAG="CPU")
            self.assertEqual(0, result.returncode, result.stderr)
            self.assertEqual("SOPERATOR_NODE_PLATFORM_TAG=CPU\n", metadata_file.read_text())

    def test_does_not_detect_or_validate_hardware(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            metadata_file = Path(tmpdir) / "node_metadata.env"
            result = self.run_writer(metadata_file, NODESET_GPU_ENABLED="true", SOPERATOR_NODE_REAL_MEMORY_BYTES="invalid")
            self.assertEqual(0, result.returncode, result.stderr)
            self.assertEqual({"NODESET_GPU_ENABLED": "true", "SOPERATOR_NODE_REAL_MEMORY_BYTES": "invalid"},
                             dict(line.split("=", 1) for line in metadata_file.read_text().splitlines()))


if __name__ == "__main__":
    unittest.main(verbosity=2)
