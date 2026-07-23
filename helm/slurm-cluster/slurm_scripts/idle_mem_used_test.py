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
        node_real_memory_bytes: int | None = 56 * GIB,
    ) -> subprocess.CompletedProcess[str]:
        with tempfile.TemporaryDirectory() as tmpdir:
            scontrol_path = Path(tmpdir) / "scontrol"
            scontrol_path.write_text(
                "#!/bin/bash\n"
                'if [[ "$*" != "listjobs --json" ]]; then\n'
                '    printf \'Unexpected arguments: %s\\n\' "$*" >&2\n'
                "    exit 64\n"
                "fi\n"
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
            if node_real_memory_bytes is None:
                env.pop("CHECKS_NODE_REAL_MEM_BYTES", None)
            else:
                env["CHECKS_NODE_REAL_MEM_BYTES"] = str(node_real_memory_bytes)
            env.update(
                {
                    "PATH": f"{tmpdir}:{env['PATH']}",
                    "SLURMD_NODENAME": "worker-1",
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
            listjobs_output='{"jobs":[{"job_id":1011}],"errors":[]}',
            total_bytes=None,
            available_bytes=None,
        )

        self.assertEqual(0, result.returncode)
        self.assertIn("Node is idle: false", result.stdout)
        self.assertIn("Local Slurm job count from JSON .jobs array: 1", result.stdout)
        self.assertIn("JSON .jobs array contains local jobs", result.stdout)
        self.assertIn("Node has local jobs; skipping memory validation", result.stdout)
        self.assertNotIn("Memory measurements:", result.stdout)

    def test_idle_node_with_enough_available_memory_passes(self):
        result = self.run_check(
            listjobs_rc=0,
            listjobs_output='{"jobs":[],"errors":[]}',
            total_bytes=64 * GIB,
            available_bytes=60 * GIB,
        )

        self.assertEqual(0, result.returncode)
        self.assertIn("Node is idle: true", result.stdout)
        self.assertIn("Local Slurm job count from JSON .jobs array: 0", result.stdout)
        self.assertIn("JSON .jobs array is empty", result.stdout)
        self.assertIn(f"used=total-available=4.29 GB ({4 * GIB} bytes)", result.stdout)
        self.assertIn(
            f"Slurm RealMemory: 60.13 GB ({56 * GIB} bytes)", result.stdout
        )
        self.assertIn(
            f"Derived maximum idle used memory: MemTotal-RealMemory=8.59 GB ({8 * GIB} bytes)",
            result.stdout,
        )

    def test_idle_node_with_insufficient_available_memory_fails(self):
        result = self.run_check(
            listjobs_rc=0,
            listjobs_output='{"jobs":[],"errors":[]}',
            total_bytes=64 * GIB,
            available_bytes=22 * GIB,
        )

        self.assertEqual(1, result.returncode)
        self.assertIn("Node is idle: true", result.stdout)
        self.assertIn("Node worker-1 is IDLE but has only 23.62 GB", result.stdout)
        self.assertIn("below Slurm RealMemory 60.13 GB by 36.51 GB", result.stdout)
        self.assertIn("derived maximum idle usage: 8.59 GB", result.stdout)
        self.assertIn("leftover or spurious processes", result.stdout)

    def test_unavailable_memory_data_does_not_drain_node(self):
        result = self.run_check(
            listjobs_rc=0,
            listjobs_output='{"jobs":[],"errors":[]}',
            total_bytes=None,
            available_bytes=None,
        )

        self.assertEqual(0, result.returncode)
        self.assertIn("Could not determine valid MemTotal and MemAvailable", result.stderr)

    def test_unavailable_real_memory_does_not_drain_node(self):
        result = self.run_check(
            listjobs_rc=0,
            listjobs_output='{"jobs":[],"errors":[]}',
            total_bytes=None,
            available_bytes=None,
            node_real_memory_bytes=None,
        )

        self.assertEqual(0, result.returncode)
        self.assertIn("Invalid or unavailable Slurm RealMemory", result.stderr)
        self.assertNotIn("Memory measurements:", result.stdout)

    def test_real_memory_larger_than_memtotal_does_not_drain_node(self):
        result = self.run_check(
            listjobs_rc=0,
            listjobs_output='{"jobs":[],"errors":[]}',
            total_bytes=64 * GIB,
            available_bytes=60 * GIB,
            node_real_memory_bytes=72 * GIB,
        )

        self.assertEqual(0, result.returncode)
        self.assertIn("exceeds MemTotal", result.stderr)
        self.assertNotIn("Derived maximum idle used memory", result.stdout)

    def test_unexpected_listjobs_failure_does_not_validate_memory(self):
        result = self.run_check(
            listjobs_rc=2,
            listjobs_output="Unable to inspect local jobs",
            total_bytes=None,
            available_bytes=None,
        )

        self.assertEqual(0, result.returncode)
        self.assertIn("scontrol listjobs --json exit code: 2", result.stdout)
        self.assertIn("scontrol listjobs --json' failed", result.stderr)
        self.assertNotIn("Memory measurements:", result.stdout)

    def test_invalid_listjobs_json_does_not_validate_memory(self):
        result = self.run_check(
            listjobs_rc=0,
            listjobs_output="not JSON",
            total_bytes=None,
            available_bytes=None,
        )

        self.assertEqual(0, result.returncode)
        self.assertIn("returned invalid job data", result.stderr)
        self.assertNotIn("Node is idle:", result.stdout)
        self.assertNotIn("Memory measurements:", result.stdout)

    def test_missing_jobs_array_does_not_validate_memory(self):
        result = self.run_check(
            listjobs_rc=0,
            listjobs_output='{"jobs":null,"errors":[]}',
            total_bytes=None,
            available_bytes=None,
        )

        self.assertEqual(0, result.returncode)
        self.assertIn("returned invalid job data", result.stderr)
        self.assertNotIn("Node is idle:", result.stdout)
        self.assertNotIn("Memory measurements:", result.stdout)


if __name__ == "__main__":
    unittest.main(verbosity=2)
