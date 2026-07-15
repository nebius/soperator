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
        listjobs_rc: int,
        listjobs_output: str,
        total_bytes: int | None,
        available_bytes: int | None,
        max_used_gb: int = 32,
    ) -> subprocess.CompletedProcess[str]:
        with tempfile.TemporaryDirectory() as tmpdir:
            scontrol_path = Path(tmpdir) / "scontrol"
            scontrol_path.write_text(
                "#!/bin/bash\n"
                "printf '%s\\n' \"${MOCK_SCONTROL_LISTJOBS_OUTPUT}\"\n"
                "exit \"${MOCK_SCONTROL_LISTJOBS_RC}\"\n",
                encoding="utf-8",
            )
            scontrol_path.chmod(0o755)

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
                    "PATH": f"{tmpdir}:{env['PATH']}",
                    "SLURMD_NODENAME": "worker-1",
                    "IDLE_MEM_USED_MAX_USED_GB": str(max_used_gb),
                    "IDLE_MEM_USED_MEMINFO_PATH": str(meminfo_path),
                    "MOCK_SCONTROL_LISTJOBS_RC": str(listjobs_rc),
                    "MOCK_SCONTROL_LISTJOBS_OUTPUT": listjobs_output,
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
            listjobs_rc=0,
            listjobs_output="JOBID\n1011",
            total_bytes=None,
            available_bytes=None,
        )

        self.assertEqual(0, result.returncode)
        self.assertIn("Node is idle: false", result.stdout)
        self.assertIn("Local Slurm jobs are present", result.stdout)
        self.assertIn("Node has local jobs; skipping memory validation", result.stdout)
        self.assertNotIn("Memory measurements:", result.stdout)

    def test_idle_node_below_threshold_passes(self):
        result = self.run_check(
            listjobs_rc=1,
            listjobs_output="No slurmstepd's found on this node",
            total_bytes=64 * GIB,
            available_bytes=60 * GIB,
        )

        self.assertEqual(0, result.returncode)
        self.assertIn("Node is idle: true", result.stdout)
        self.assertIn("No local slurmstepd was found", result.stdout)
        self.assertIn(f"used=total-available=4.29 GB ({4 * GIB} bytes)", result.stdout)
        self.assertIn("Maximum allowed used memory: 32 GB (32000000000 bytes)", result.stdout)

    def test_idle_node_above_threshold_fails_with_actionable_reason(self):
        result = self.run_check(
            listjobs_rc=1,
            listjobs_output="No slurmstepd's found on this node",
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
            listjobs_rc=1,
            listjobs_output="No slurmstepd's found on this node",
            total_bytes=None,
            available_bytes=None,
        )

        self.assertEqual(0, result.returncode)
        self.assertIn("Could not determine valid MemTotal and MemAvailable", result.stderr)

    def test_unexpected_listjobs_failure_does_not_validate_memory(self):
        result = self.run_check(
            listjobs_rc=2,
            listjobs_output="Unable to inspect local jobs",
            total_bytes=None,
            available_bytes=None,
        )

        self.assertEqual(0, result.returncode)
        self.assertIn("scontrol listjobs exit code: 2", result.stdout)
        self.assertIn("Could not determine whether the node is idle", result.stderr)
        self.assertNotIn("Memory measurements:", result.stdout)


if __name__ == "__main__":
    unittest.main(verbosity=2)
