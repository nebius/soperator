import os
import subprocess
import tempfile
import unittest
from pathlib import Path


SCRIPT_PATH = Path(__file__).with_name("idle_mem_used.sh")
GIB = 1024 * 1024 * 1024


class IdleMemUsedTest(unittest.TestCase):
    def run_check(
        self,
        *,
        node_states: str,
        total_bytes: int | None,
        available_bytes: int | None,
        max_used_gb: int = 32,
    ) -> subprocess.CompletedProcess[str]:
        with tempfile.TemporaryDirectory() as tmpdir:
            meminfo_path = Path(tmpdir) / "meminfo"
            if total_bytes is not None and available_bytes is not None:
                meminfo_path.write_text(
                    f"MemTotal:       {total_bytes // 1024} kB\n"
                    f"MemAvailable:   {available_bytes // 1024} kB\n",
                    encoding="utf-8",
                )

            env = os.environ.copy()
            env.update(
                {
                    "SLURMD_NODENAME": "worker-1",
                    "CHECKS_NODE_STATE_FLAGS": node_states,
                    "IDLE_MEM_USED_MAX_USED_GB": str(max_used_gb),
                    "IDLE_MEM_USED_MEMINFO_PATH": str(meminfo_path),
                }
            )

            return subprocess.run(
                [
                    "bash",
                    "-c",
                    'exec 3>&1; exec bash "$1"',
                    "idle-mem-used-test",
                    str(SCRIPT_PATH),
                ],
                check=False,
                env=env,
                stdout=subprocess.PIPE,
                stderr=subprocess.PIPE,
                text=True,
            )

    def test_non_idle_node_skips_memory_validation(self):
        result = self.run_check(
            node_states="ALLOCATED",
            total_bytes=None,
            available_bytes=None,
        )

        self.assertEqual(0, result.returncode)
        self.assertIn("Node is idle: false", result.stdout)
        self.assertIn("Node is not IDLE; skipping memory validation", result.stdout)
        self.assertNotIn("Memory measurements:", result.stdout)

    def test_idle_node_below_threshold_passes(self):
        result = self.run_check(
            node_states="IDLE",
            total_bytes=64 * GIB,
            available_bytes=60 * GIB,
        )

        self.assertEqual(0, result.returncode)
        self.assertIn("Node is idle: true", result.stdout)
        self.assertIn(f"used=total-available=4.29 GB ({4 * GIB} bytes)", result.stdout)
        self.assertIn("Maximum allowed used memory: 32 GB (32000000000 bytes)", result.stdout)

    def test_idle_node_above_threshold_fails_with_actionable_reason(self):
        result = self.run_check(
            node_states="IDLE+CLOUD",
            total_bytes=64 * GIB,
            available_bytes=22 * GIB,
        )

        self.assertEqual(1, result.returncode)
        self.assertIn("Node is idle: true", result.stdout)
        self.assertIn("Node worker-1 is IDLE but uses", result.stdout)
        self.assertIn("45.10 GB", result.stdout)
        self.assertIn("threshold: 32 GB", result.stdout)
        self.assertIn("leftover or spurious processes", result.stdout)

    def test_unavailable_memory_data_does_not_drain_node(self):
        result = self.run_check(
            node_states="IDLE",
            total_bytes=None,
            available_bytes=None,
        )

        self.assertEqual(0, result.returncode)
        self.assertIn("Could not determine valid MemTotal and MemAvailable", result.stderr)


if __name__ == "__main__":
    unittest.main(verbosity=2)
