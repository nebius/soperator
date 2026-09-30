import os
import stat
import subprocess
import tempfile
import textwrap
import unittest
from pathlib import Path


IMPORTER_PATH = Path(__file__).with_name("pyxis_caching_importer.sh")
DIGEST = "sha256-test"


FAKE_ENROOT = """\
#!/usr/bin/env python3
import fcntl
import os
import sys
import time
from pathlib import Path

command = sys.argv[1]
if command == "digest":
    print(os.environ["FAKE_ENROOT_DIGEST"])
    sys.exit(0)

if command != "import":
    raise SystemExit(f"unsupported enroot command: {command}")

output = Path(sys.argv[sys.argv.index("--output") + 1])
count_path = Path(os.environ["FAKE_ENROOT_IMPORT_COUNT"])
with count_path.open("a+") as count_file:
    fcntl.flock(count_file.fileno(), fcntl.LOCK_EX)
    count_file.seek(0)
    count = int(count_file.read() or "0") + 1
    count_file.seek(0)
    count_file.truncate()
    count_file.write(str(count))
    count_file.flush()

output.write_text("incomplete")
if os.environ.get("FAKE_ENROOT_FAIL") == "1":
    sys.exit(1)
time.sleep(float(os.environ.get("FAKE_ENROOT_IMPORT_DELAY", "0")))
output.write_text("complete")
"""


FAKE_FLOCK = """\
#!/usr/bin/env python3
import fcntl
import os
import sys

arguments = sys.argv[1:]
operation = fcntl.LOCK_UN
if "-x" in arguments:
    operation = fcntl.LOCK_EX
elif "-s" in arguments:
    operation = fcntl.LOCK_SH
if "-n" in arguments:
    operation |= fcntl.LOCK_NB

fd = int(arguments[-1])
try:
    fcntl.flock(fd, operation)
except BlockingIOError:
    sys.exit(1)

log_path = os.environ.get("FAKE_FLOCK_LOG")
if log_path:
    with open(log_path, "a") as log:
        log.write(" ".join(arguments) + "\\n")
"""


class PyxisCachingImporterTest(unittest.TestCase):
    def setUp(self):
        self.temporary_directory = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary_directory.cleanup)
        self.root = Path(self.temporary_directory.name)
        self.cache = self.root / "cache"
        self.cache.mkdir()
        self.bin = self.root / "bin"
        self.bin.mkdir()
        self.import_count = self.root / "import-count"
        self.flock_log = self.root / "flock.log"

        self.write_executable("enroot", FAKE_ENROOT)
        self.write_executable("flock", FAKE_FLOCK)
        self.write_executable("setfattr", "#!/bin/sh\nexit 0\n")

        self.environment = os.environ.copy()
        self.environment.update(
            {
                "PATH": f"{self.bin}{os.pathsep}{self.environment['PATH']}",
                "ENROOT_CONTAINER_IMAGES_CACHE_DIR": str(self.cache),
                "FAKE_ENROOT_DIGEST": DIGEST,
                "FAKE_ENROOT_IMPORT_COUNT": str(self.import_count),
                "FAKE_FLOCK_LOG": str(self.flock_log),
                "SLURM_JOB_ID": "100",
                "SLURM_STEP_ID": "0",
            }
        )

    def write_executable(self, name, contents):
        path = self.bin / name
        path.write_text(textwrap.dedent(contents))
        path.chmod(0o755)

    def run_get(self, node):
        environment = self.environment.copy()
        environment["SLURMD_NODENAME"] = node
        return subprocess.Popen(
            ["bash", str(IMPORTER_PATH), "get", "docker://example/image:tag"],
            env=environment,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            text=True,
        )

    def collect(self, processes):
        results = [process.communicate(timeout=10) for process in processes]
        for process, (_, stderr) in zip(processes, results):
            self.assertEqual(0, process.returncode, stderr)
        return results

    def test_warm_cache_readers_do_not_call_flock(self):
        final_path = self.cache / f"{DIGEST}.sqsh"
        final_path.write_text("complete")

        results = self.collect([self.run_get(f"node-{index}") for index in range(10)])

        self.assertEqual(
            [str(final_path)] * 10,
            [stdout.strip() for stdout, _ in results],
        )
        self.assertFalse(self.import_count.exists())
        self.assertFalse(self.flock_log.exists())

    def test_cold_cache_has_one_producer_and_concurrent_waiters(self):
        self.environment["FAKE_ENROOT_IMPORT_DELAY"] = "0.3"

        results = self.collect([self.run_get(f"node-{index}") for index in range(10)])

        final_path = self.cache / f"{DIGEST}.sqsh"
        self.assertEqual("1", self.import_count.read_text())
        self.assertEqual("complete", final_path.read_text())
        self.assertEqual(
            0o666,
            stat.S_IMODE((self.cache / f"{DIGEST}.sqsh.lock").stat().st_mode),
        )
        self.assertEqual(
            [str(final_path)] * 10,
            [stdout.strip() for stdout, _ in results],
        )

        flock_calls = self.flock_log.read_text().splitlines()
        self.assertIn("-n -x 9", flock_calls)
        self.assertIn("-s 9", flock_calls)

    def test_release_removes_only_the_calling_nodes_temporary_file(self):
        node_1_temp = self.cache / "100.0.node-1.sqsh"
        node_2_temp = self.cache / "100.0.node-2.sqsh"
        node_1_temp.write_text("node 1")
        node_2_temp.write_text("node 2")
        environment = self.environment.copy()
        environment["SLURMD_NODENAME"] = "node-1"

        result = subprocess.run(
            ["bash", str(IMPORTER_PATH), "release"],
            env=environment,
            capture_output=True,
            text=True,
            timeout=10,
        )

        self.assertEqual(0, result.returncode, result.stderr)
        self.assertFalse(node_1_temp.exists())
        self.assertTrue(node_2_temp.exists())
        self.assertFalse(self.flock_log.exists())

    def test_failed_import_does_not_publish_or_leave_a_temporary_file(self):
        self.environment["FAKE_ENROOT_FAIL"] = "1"

        process = self.run_get("node-1")
        _, stderr = process.communicate(timeout=10)

        self.assertNotEqual(0, process.returncode, stderr)
        self.assertFalse((self.cache / f"{DIGEST}.sqsh").exists())
        self.assertFalse((self.cache / "100.0.node-1.sqsh").exists())


if __name__ == "__main__":
    unittest.main(verbosity=2)
