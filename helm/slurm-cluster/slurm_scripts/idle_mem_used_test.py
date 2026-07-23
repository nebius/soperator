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
        free_bytes_rc: int = 0,
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

            free_path = Path(tmpdir) / "free"
            free_path.write_text(
                "#!/bin/bash\n"
                'case "$*" in\n'
                '    "-b")\n'
                '        if (( MOCK_FREE_BYTES_RC != 0 )); then\n'
                '            printf "Unable to read local memory\\n" >&2\n'
                '            exit "${MOCK_FREE_BYTES_RC}"\n'
                "        fi\n"
                '        printf "              total        used        free      shared  buff/cache   available\\n"\n'
                '        printf "Mem:          %s           0           0           0           0  %s\\n" \\\n'
                '            "${MOCK_FREE_TOTAL_BYTES}" "${MOCK_FREE_AVAILABLE_BYTES}"\n'
                "        ;;\n"
                '    "-hw")\n'
                '        printf "               total        used        free      shared     buffers       cache   available\\n"\n'
                '        printf "Mem:            mock        mock        mock        mock        mock        mock        mock\\n"\n'
                "        ;;\n"
                "    *)\n"
                '        printf \'Unexpected arguments: %s\\n\' "$*" >&2\n'
                "        exit 64\n"
                "        ;;\n"
                "esac\n",
                encoding="utf-8",
            )
            free_path.chmod(0o755)

            env = os.environ.copy()
            if node_real_memory_bytes is None:
                env.pop("CHECKS_NODE_REAL_MEM_BYTES", None)
            else:
                env["CHECKS_NODE_REAL_MEM_BYTES"] = str(node_real_memory_bytes)
            env.update(
                {
                    "PATH": f"{tmpdir}:{env['PATH']}",
                    "SLURMD_NODENAME": "worker-1",
                    "MOCK_SCONTROL_LISTJOBS_RC": str(listjobs_rc),
                    "MOCK_SCONTROL_LISTJOBS_OUTPUT": listjobs_output,
                    "MOCK_FREE_TOTAL_BYTES": (
                        str(total_bytes) if total_bytes is not None else ""
                    ),
                    "MOCK_FREE_AVAILABLE_BYTES": (
                        str(available_bytes) if available_bytes is not None else ""
                    ),
                    "MOCK_FREE_BYTES_RC": str(free_bytes_rc),
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
        self.assertNotIn("Memory comparison:", result.stdout)

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
        self.assertIn(
            f"available=64.42 GB ({60 * GIB} bytes)", result.stdout
        )
        self.assertIn(
            f"Slurm RealMemory=60.13 GB ({56 * GIB} bytes)",
            result.stdout,
        )
        self.assertIn("Memory snapshot (free -hw):", result.stdout)
        self.assertIn("Mem:            mock", result.stdout)

    def test_idle_node_with_insufficient_available_memory_fails(self):
        result = self.run_check(
            listjobs_rc=0,
            listjobs_output='{"jobs":[],"errors":[]}',
            total_bytes=64 * GIB,
            available_bytes=22 * GIB,
        )

        self.assertEqual(1, result.returncode)
        self.assertIn("Node is idle: true", result.stdout)
        self.assertIn(
            "available memory 23.62 GB < Slurm RealMemory 60.13 GB", result.stdout
        )
        self.assertIn("stop leftover processes or reboot", result.stdout)

    def test_unavailable_memory_data_does_not_drain_node(self):
        result = self.run_check(
            listjobs_rc=0,
            listjobs_output='{"jobs":[],"errors":[]}',
            total_bytes=None,
            available_bytes=None,
        )

        self.assertEqual(0, result.returncode)
        self.assertIn(
            "Could not determine valid total and available memory", result.stderr
        )

    def test_free_failure_does_not_drain_node(self):
        result = self.run_check(
            listjobs_rc=0,
            listjobs_output='{"jobs":[],"errors":[]}',
            total_bytes=None,
            available_bytes=None,
            free_bytes_rc=2,
        )

        self.assertEqual(0, result.returncode)
        self.assertIn("Could not read local memory information", result.stderr)
        self.assertNotIn("Memory comparison:", result.stdout)

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
        self.assertNotIn("Memory comparison:", result.stdout)

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
        self.assertNotIn("Memory comparison:", result.stdout)

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
        self.assertNotIn("Memory comparison:", result.stdout)

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
        self.assertNotIn("Memory comparison:", result.stdout)

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
        self.assertNotIn("Memory comparison:", result.stdout)


if __name__ == "__main__":
    unittest.main(verbosity=2)
