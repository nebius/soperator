import os
import stat
import subprocess
import tempfile
import unittest
from pathlib import Path


SCRIPT_PATH = Path(__file__).with_name("write_soperator_metadata.sh")


class WriteSoperatorMetadataTest(unittest.TestCase):
    def run_writer(
        self, metadata_file: Path, real_memory_bytes: str | None
    ) -> subprocess.CompletedProcess[str]:
        env = os.environ.copy()
        if real_memory_bytes is None:
            env.pop("SOPERATOR_NODE_REAL_MEMORY_BYTES", None)
        else:
            env["SOPERATOR_NODE_REAL_MEMORY_BYTES"] = real_memory_bytes

        return subprocess.run(
            ["bash", str(SCRIPT_PATH), str(metadata_file)],
            check=False,
            env=env,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )

    def test_writes_real_memory_metadata_atomically(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            metadata_file = Path(tmpdir) / "nested" / "node_metadata.env"

            result = self.run_writer(metadata_file, "999292928")

            self.assertEqual(0, result.returncode)
            self.assertEqual(
                "SOPERATOR_NODE_REAL_MEMORY_BYTES=999292928\n",
                metadata_file.read_text(encoding="utf-8"),
            )
            self.assertEqual(0o644, stat.S_IMODE(metadata_file.stat().st_mode))
            self.assertEqual([], list(metadata_file.parent.glob("*.tmp.*")))

    def test_replaces_existing_metadata(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            metadata_file = Path(tmpdir) / "node_metadata.env"
            metadata_file.write_text(
                "SOPERATOR_NODE_REAL_MEMORY_BYTES=1\n", encoding="utf-8"
            )

            result = self.run_writer(metadata_file, "2147483648")

            self.assertEqual(0, result.returncode)
            self.assertEqual(
                "SOPERATOR_NODE_REAL_MEMORY_BYTES=2147483648\n",
                metadata_file.read_text(encoding="utf-8"),
            )

    def test_missing_or_invalid_metadata_is_skipped(self):
        for value in (None, "", "0", "-1", "1.5", "invalid"):
            with self.subTest(value=value), tempfile.TemporaryDirectory() as tmpdir:
                metadata_file = Path(tmpdir) / "node_metadata.env"
                metadata_file.write_text(
                    "SOPERATOR_NODE_REAL_MEMORY_BYTES=1\n", encoding="utf-8"
                )

                result = self.run_writer(metadata_file, value)

                self.assertEqual(0, result.returncode)
                self.assertFalse(metadata_file.exists())
                self.assertIn("skipping node metadata export", result.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
