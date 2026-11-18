import concurrent.futures
import os
import subprocess
import tempfile
import unittest
from pathlib import Path


PLUGIN = Path(__file__).resolve().parents[1]
HOOK = PLUGIN.parent / "enroot" / "30-nccl-debug.sh"
STATE = "_SNCCLD_ENROOT_STATE_DIR"
LOG = "_SNCCLD_ENROOT_LOG_DIR"


class EnrootHookTest(unittest.TestCase):
    def invoke(self, environment):
        return subprocess.run(["bash", str(HOOK)], env={"PATH": os.environ["PATH"], **environment},
                              capture_output=True, text=True)

    def test_inactive_hook_does_not_require_runtime_file(self):
        for environment in ({}, {LOG: "/logs"}, {"SNCCLD_ENABLED": "1"}, {STATE: ""}):
            with self.subTest(environment=environment):
                result = self.invoke(environment)
                self.assertEqual(0, result.returncode, result.stderr)

    def test_mounts_are_appended_to_current_runtime(self):
        with tempfile.TemporaryDirectory() as directory:
            mounts = Path(directory) / "fstab"
            mounts.write_text("existing mount\n")
            result = self.invoke({STATE: "/tmp/nccl_debug", LOG: "/logs/worker-0", "ENROOT_MOUNTS": str(mounts)})
            self.assertEqual(0, result.returncode, result.stderr)
            self.assertEqual("existing mount\n/tmp/nccl_debug /tmp/nccl_debug none x-create=dir,bind,rw,nosuid\n"
                             "/logs/worker-0 /logs/worker-0 none x-create=dir,bind,rw,nosuid\n", mounts.read_text())

    def test_omits_empty_and_duplicate_log_directory(self):
        for log in ("", "/tmp/state"):
            with self.subTest(log=log), tempfile.TemporaryDirectory() as directory:
                mounts = Path(directory) / "fstab"
                result = self.invoke({STATE: "/tmp/state", LOG: log, "ENROOT_MOUNTS": str(mounts)})
                self.assertEqual(0, result.returncode, result.stderr)
                self.assertEqual("/tmp/state /tmp/state none x-create=dir,bind,rw,nosuid\n", mounts.read_text())

    def test_escapes_fstab_paths_without_shell_evaluation(self):
        with tempfile.TemporaryDirectory() as directory:
            mounts = Path(directory) / "fstab"
            sentinel = Path(directory) / "must-not-exist"
            value = "/logs/a b\\c\td\ne/$(touch " + str(sentinel) + ")"
            escaped = value.replace("\\", "\\134").replace(" ", "\\040").replace("\t", "\\011").replace("\n", "\\012")
            result = self.invoke({STATE: value, "ENROOT_MOUNTS": str(mounts)})
            self.assertEqual(0, result.returncode, result.stderr)
            self.assertEqual(f"{escaped} {escaped} none x-create=dir,bind,rw,nosuid\n", mounts.read_text())
            self.assertFalse(sentinel.exists())

    def test_concurrent_containers_have_independent_mount_lists(self):
        with tempfile.TemporaryDirectory() as directory:
            def start(index):
                mounts = Path(directory) / str(index)
                result = self.invoke({STATE: f"/state/{index}", LOG: f"/logs/{index}", "ENROOT_MOUNTS": str(mounts)})
                self.assertEqual(0, result.returncode, result.stderr)
                self.assertEqual([f"/state/{index}", f"/logs/{index}"],
                                 [line.split()[0] for line in mounts.read_text().splitlines()])

            with concurrent.futures.ThreadPoolExecutor(max_workers=8) as executor:
                list(executor.map(start, range(32)))

    def test_active_hook_reports_unwritable_runtime(self):
        result = self.invoke({STATE: "/state", "ENROOT_MOUNTS": "/missing-parent/sched3078/fstab"})
        self.assertNotEqual(0, result.returncode)

    def test_spank_environment_publication_and_error_handling(self):
        with tempfile.TemporaryDirectory() as directory:
            binary = str(Path(directory) / "enroot-env-test")
            subprocess.run(["cc", "-std=c99", "-Wall", "-Wextra", "-Werror",
                            "-I" + str(PLUGIN / "tests" / "include"), "-I" + str(PLUGIN / "src"),
                            str(PLUGIN / "src" / "snccld_enroot.c"), str(PLUGIN / "tests" / "enroot_env_test.c"),
                            "-o", binary], check=True, capture_output=True, text=True)
            result = subprocess.run([binary], check=True, capture_output=True, text=True)
            self.assertIn("passed", result.stdout)


if __name__ == "__main__":
    unittest.main(verbosity=2)
