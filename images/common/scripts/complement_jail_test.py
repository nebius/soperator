import fcntl
import io
import json
import os
import signal
import stat
import subprocess
import sys
import tempfile
import threading
import time
import unittest
from contextlib import contextmanager, redirect_stdout
from pathlib import Path
from unittest.mock import patch

import complement_jail as cj

ARCH = "x86_64"
VERSION = "580.65.06"


def quiet(test):
    """Silence the script's log lines while a test runs."""

    def wrapper(self):
        with redirect_stdout(io.StringIO()):
            test(self)

    return wrapper


class JailTestCase(unittest.TestCase):
    def setUp(self):
        self.tmp = Path(tempfile.mkdtemp()).resolve()
        self.addCleanup(lambda: subprocess.run(["rm", "-rf", str(self.tmp)], check=True))
        self.jail = cj.Jail(self.tmp / "jail", ARCH)
        for rel in ("etc", "var/lib/dpkg", "usr/bin", f"usr/lib/{ARCH}-linux-gnu"):
            (self.jail.root / rel).mkdir(parents=True)
        (self.jail.root / "lib").symlink_to("usr/lib")

    def host_file(self, rel, content="x" * 4096, mode=0o644):
        path = self.tmp / "host" / rel
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content)
        path.chmod(mode)
        return str(path)

    def jail_file(self, rel, content="x" * 4096):
        path = self.jail.path(rel)
        path.parent.mkdir(parents=True, exist_ok=True)
        path.write_text(content)
        return path


class DriverCapabilityFlagsTest(unittest.TestCase):
    def test_default_capabilities(self):
        self.assertEqual(["--compute", "--utility"], cj.driver_capability_flags("compute,utility"))

    def test_all_expands_to_every_capability(self):
        self.assertEqual(
            ["--compute", "--compat32", "--display", "--graphics", "--utility", "--video"],
            cj.driver_capability_flags("all"),
        )

    def test_whitespace_around_commas_is_tolerated(self):
        self.assertEqual(["--compute", "--utility"], cj.driver_capability_flags("compute, utility,"))

    def test_unknown_capability_is_fatal(self):
        with self.assertRaisesRegex(cj.Fatal, "Unknown NVIDIA driver capability: bogus"):
            cj.driver_capability_flags("compute,bogus")


class MountTargetsTest(JailTestCase):
    def test_libraries_are_flattened_into_the_multiarch_dir(self):
        for host in (f"/usr/lib/{ARCH}-linux-gnu/libcuda.so.{VERSION}", f"/usr/lib/{ARCH}-linux-gnu/vdpau/libvdpau_nvidia.so.{VERSION}"):
            self.assertEqual(self.jail.lib_dir / Path(host).name, cj.jail_mount_target(self.jail, host))

    def test_other_files_keep_their_path_with_jail_symlinks_resolved(self):
        self.assertEqual(self.jail.path("usr/bin/nvidia-smi"), cj.jail_mount_target(self.jail, "/usr/bin/nvidia-smi"))
        self.assertEqual(
            self.jail.path(f"usr/lib/firmware/nvidia/{VERSION}/gsp.bin"),
            cj.jail_mount_target(self.jail, f"/lib/firmware/nvidia/{VERSION}/gsp.bin"),
        )

    def test_targets_come_from_the_cli_listing(self):
        with patch.object(cj, "output", return_value=f"/usr/bin/nvidia-smi\n\n/usr/lib/{ARCH}-linux-gnu/libcuda.so.1\n"):
            targets = cj.gpu_mount_targets(self.jail)
        self.assertEqual(
            [
                cj.MountTarget("/usr/bin/nvidia-smi", self.jail.path("usr/bin/nvidia-smi")),
                cj.MountTarget(f"/usr/lib/{ARCH}-linux-gnu/libcuda.so.1", self.jail.lib_dir / "libcuda.so.1"),
            ],
            targets,
        )

    def test_failed_listing_is_fatal(self):
        with patch.object(cj, "output", side_effect=subprocess.CalledProcessError(1, "nvidia-container-cli")):
            with self.assertRaisesRegex(cj.Fatal, "nvidia-container-cli list failed, exiting so the container restarts"):
                cj.gpu_mount_targets(self.jail)


class PlaceholdersTest(JailTestCase):
    @quiet
    def test_placeholders_copy_the_host_mode_and_are_created_once(self):
        lib = self.host_file(f"usr/lib/{ARCH}-linux-gnu/libcuda.so.{VERSION}")
        binary = self.host_file("usr/local/bin/nvidia-smi", mode=0o755)
        targets = [cj.MountTarget(lib, self.jail.lib_dir / f"libcuda.so.{VERSION}"), cj.MountTarget(binary, self.jail.path("usr/local/bin/nvidia-smi"))]

        cj.precreate_gpu_placeholders(self.jail, targets)

        for target in targets:
            self.assertEqual(cj.PLACEHOLDER_MARKER, target.jail.read_text())
        self.assertEqual(0o644, stat.S_IMODE(targets[0].jail.stat().st_mode))
        self.assertEqual(0o755, stat.S_IMODE(targets[1].jail.stat().st_mode))

        before = {target.jail: target.jail.stat().st_mtime_ns for target in targets}
        cj.precreate_gpu_placeholders(self.jail, targets)
        self.assertEqual(before, {target.jail: target.jail.stat().st_mtime_ns for target in targets})

    @quiet
    def test_existing_placeholder_is_never_truncated(self):
        existing = self.jail_file(f"usr/lib/{ARCH}-linux-gnu/libcuda.so.{VERSION}")
        self.assertFalse(cj.create_placeholder(cj.MountTarget("/nonexistent/libcuda.so", existing)))
        self.assertEqual("x" * 4096, existing.read_text())

    @quiet
    def test_unreadable_host_file_still_gets_a_placeholder(self):
        target = cj.MountTarget("/nonexistent/gsp.bin", self.jail.path("usr/lib/firmware/gsp.bin"))
        cj.precreate_gpu_placeholders(self.jail, [target])
        self.assertEqual(cj.PLACEHOLDER_MARKER, target.jail.read_text())


class MountGPULibsTest(JailTestCase):
    def setUp(self):
        super().setUp()
        self.addCleanup(patch.object(cj, "dump_nvml_state").start().stop)

    def run_mount(self, returncode, mounted):
        with patch.object(cj, "run", return_value=subprocess.CompletedProcess([], returncode)) as run:
            with patch.object(cj, "mount_points_under", side_effect=[set(), mounted]):
                cj.mount_gpu_libs(self.jail, ["--compute", "--utility"])
        return run.call_args.args[0]

    @quiet
    def test_verifies_the_new_mounts(self):
        lib = self.jail_file(f"usr/lib/{ARCH}-linux-gnu/libcuda.so.{VERSION}")
        argv = self.run_mount(0, {lib, self.jail.path("etc/nvidia/profiles.d")})
        self.assertEqual(f"--ldconfig={cj.FAKE_LDCONFIG}", argv[argv.index("--device=all") - 1])
        self.assertEqual(["--compute", "--utility", self.jail.root], argv[-3:])

    @quiet
    def test_cli_failure_is_fatal(self):
        with self.assertRaisesRegex(cj.Fatal, "nvidia-container-cli failed"):
            self.run_mount(1, set())

    @quiet
    def test_missing_mounts_are_an_anomaly(self):
        with self.assertRaisesRegex(cj.Fatal, "no new jail mounts after nvidia-container-cli"):
            self.run_mount(0, set())

    @quiet
    def test_mounted_file_without_content_is_an_anomaly(self):
        lib = self.jail_file(f"usr/lib/{ARCH}-linux-gnu/libcuda.so.{VERSION}", "tiny")
        with self.assertRaisesRegex(cj.Fatal, f"{lib} shows no real content right after mounting"):
            self.run_mount(0, {lib})


class LinkerCacheTest(JailTestCase):
    def setUp(self):
        super().setUp()
        self.ldconfig = patch.object(cj.Jail, "ldconfig", return_value=subprocess.CompletedProcess([], 0)).start()
        self.addCleanup(self.ldconfig.stop)

    def set_entries(self, current, candidate):
        patcher = patch.object(cj.Jail, "ldconfig_entries", side_effect=[current, candidate])
        self.addCleanup(patcher.stop)
        return patcher.start()

    @quiet
    def test_skips_when_the_cache_is_current(self):
        self.set_entries(["libc.so.6 => /x"], ["libc.so.6 => /x"])
        self.assertEqual(cj.LinkerCacheUpdate(cj.LdconfigOutcome.SKIPPED), cj.update_linker_cache(self.jail))
        self.assertEqual(["-X", "-i", "-C", f"/tmp/ld.so.cache.candidate.{os.getpid()}"], list(self.ldconfig.call_args.args))

    @quiet
    def test_rebuilds_under_the_lock_when_stale(self):
        self.set_entries([], ["libcuda.so.1 => /x"])
        self.assertEqual(cj.LinkerCacheUpdate(cj.LdconfigOutcome.RAN), cj.update_linker_cache(self.jail))
        self.assertEqual((), self.ldconfig.call_args.args, "the rebuild is a plain ldconfig run")

    @quiet
    def test_reports_a_failed_rebuild(self):
        self.set_entries([], ["libcuda.so.1 => /x"])
        self.ldconfig.return_value = subprocess.CompletedProcess([], 3)
        self.assertEqual(cj.LinkerCacheUpdate(cj.LdconfigOutcome.FAILED, 3), cj.update_linker_cache(self.jail))

    @quiet
    def test_leaves_the_rebuild_to_the_lock_holder(self):
        self.set_entries([], ["libcuda.so.1 => /x"])
        with self.jail.lock("ldconfig", timeout=0):
            self.assertEqual(cj.LinkerCacheUpdate(cj.LdconfigOutcome.LOCK_BUSY), cj.update_linker_cache(self.jail))
        self.assertEqual(1, self.ldconfig.call_count, "only the candidate build ran")


class NVMLSonameTest(JailTestCase):
    def setUp(self):
        super().setUp()
        for name in ("dump_nvml_state", "nvml_mounted"):
            self.addCleanup(patch.object(cj, name, return_value=True).start().stop)
        self.addCleanup(patch.object(cj.time, "sleep").start().stop)
        self.lib = self.jail_file(f"usr/lib/{ARCH}-linux-gnu/libnvidia-ml.so.{VERSION}")
        self.soname = self.jail.lib_dir / cj.NVML_SONAME

    def link_soname(self, *args, **kwargs):
        self.soname.symlink_to(self.lib.name)
        return subprocess.CompletedProcess([], 0)

    @quiet
    def test_usable_soname_needs_no_repair(self):
        self.link_soname()
        with patch.object(cj.Jail, "ldconfig") as ldconfig:
            cj.ensure_nvml_soname(self.jail, cj.LdconfigOutcome.RAN)
        ldconfig.assert_not_called()

    @quiet
    def test_missing_soname_is_repaired_by_a_local_ldconfig(self):
        with patch.object(cj.Jail, "ldconfig", side_effect=self.link_soname) as ldconfig:
            cj.ensure_nvml_soname(self.jail, cj.LdconfigOutcome.SKIPPED)
        ldconfig.assert_called_once()
        self.assertTrue(cj.has_real_content(self.soname))

    @quiet
    def test_unrepairable_soname_is_fatal(self):
        with patch.object(cj.Jail, "ldconfig", return_value=subprocess.CompletedProcess([], 3)):
            with self.assertRaisesRegex(cj.Fatal, "libnvidia-ml.so.1 still missing or empty after repair"):
                cj.ensure_nvml_soname(self.jail, cj.LdconfigOutcome.FAILED)

    @quiet
    def test_missing_nvml_mount_skips_the_check(self):
        cj.nvml_mounted.return_value = False
        with patch.object(cj.Jail, "ldconfig") as ldconfig:
            cj.ensure_nvml_soname(self.jail, cj.LdconfigOutcome.RAN)
        ldconfig.assert_not_called()


class DiversionsTest(JailTestCase):
    def setUp(self):
        super().setUp()
        self.added = []
        patcher = patch.object(cj.Jail, "add_diversion", autospec=True, side_effect=self.record)
        patcher.start()
        self.addCleanup(patcher.stop)

    def record(self, jail, source, target):
        self.added.append((source, target))
        with jail.path("var/lib/dpkg/diversions").open("a") as db:
            db.write(f"{source}\n{target}\n:\n")

    def test_divert_target_is_a_subdirectory_next_to_the_file(self):
        self.assertEqual("/usr/bin/soperator-diverted/nvidia-smi", cj.divert_target("/usr/bin/nvidia-smi"))

    @quiet
    def test_mount_targets_are_diverted_under_every_packaged_spelling(self):
        lib_dir = f"/usr/lib/{ARCH}-linux-gnu"
        targets = [
            cj.MountTarget(f"/usr/local/lib/libcuda.so.{VERSION}", self.jail_file(f"usr/lib/{ARCH}-linux-gnu/libcuda.so.{VERSION}")),
            cj.MountTarget(f"{lib_dir}/vdpau/libvdpau_nvidia.so.{VERSION}", self.jail_file(f"usr/lib/{ARCH}-linux-gnu/libvdpau_nvidia.so.{VERSION}")),
            cj.MountTarget("/usr/bin/nvidia-smi", self.jail_file("usr/bin/nvidia-smi")),
            cj.MountTarget(f"/lib/firmware/nvidia/{VERSION}/gsp.bin", self.jail_file(f"usr/lib/firmware/nvidia/{VERSION}/gsp.bin")),
            cj.MountTarget("/etc/nvidia/profiles.d", self.jail.path("etc/nvidia/profiles.d")),
        ]
        targets[-1].jail.mkdir(parents=True)

        cj.divert_gpu_mount_targets(self.jail, targets)

        self.assertEqual(
            sorted(
                (path, cj.divert_target(path))
                for path in (
                    f"{lib_dir}/libcuda.so.{VERSION}",
                    f"{lib_dir}/libvdpau_nvidia.so.{VERSION}",
                    "/usr/bin/nvidia-smi",
                    f"/usr/lib/firmware/nvidia/{VERSION}/gsp.bin",
                    f"/lib/firmware/nvidia/{VERSION}/gsp.bin",
                )
            ),
            sorted(self.added),
        )
        self.assertTrue(self.jail.path(f"usr/lib/{ARCH}-linux-gnu/soperator-diverted").is_dir())
        self.assertTrue(self.jail.path(f"usr/lib/firmware/nvidia/{VERSION}/soperator-diverted").is_dir())

        self.added.clear()
        cj.divert_gpu_mount_targets(self.jail, targets)
        self.assertEqual([], self.added, "existing diversions are left alone")

    @quiet
    def test_soname_symlinks_pointing_at_mounted_libraries_are_diverted(self):
        lib = self.jail_file(f"usr/lib/{ARCH}-linux-gnu/libcuda.so.{VERSION}")
        (self.jail.lib_dir / "libcuda.so.1").symlink_to(lib.name)
        (self.jail.lib_dir / "libz.so.1").symlink_to("libz.so.1.3")
        cj.divert_gpu_sonames(self.jail, [cj.MountTarget("/host/libcuda.so", lib)])
        self.assertEqual([(f"/usr/lib/{ARCH}-linux-gnu/libcuda.so.1", f"/usr/lib/{ARCH}-linux-gnu/soperator-diverted/libcuda.so.1")], self.added)

    @quiet
    def test_diversions_are_skipped_while_dpkg_holds_its_lock(self):
        hold_lock = "import fcntl, sys, time; f = open(sys.argv[1], 'a'); fcntl.lockf(f, fcntl.LOCK_EX); print('locked', flush=True); time.sleep(30)"
        apt = subprocess.Popen([sys.executable, "-c", hold_lock, str(self.jail.path("var/lib/dpkg/lock-frontend"))], stdout=subprocess.PIPE, text=True)
        self.addCleanup(apt.kill)
        self.assertEqual("locked\n", apt.stdout.readline())
        with redirect_stdout(io.StringIO()) as out:
            cj.divert_jail_paths(self.jail, ["/usr/bin/nvidia-smi"])
        self.assertEqual([], self.added)
        self.assertIn("dpkg database is in use", out.getvalue())

    @quiet
    def test_diversions_added_while_waiting_for_the_lock_are_not_added_again(self):
        path = "/usr/bin/nvidia-smi"

        @contextmanager
        def lock_after_another_worker_added_it(jail, name, *, timeout):
            self.record(jail, path, cj.divert_target(path))
            self.added.clear()
            yield True

        with patch.object(cj.Jail, "lock", lock_after_another_worker_added_it):
            cj.divert_jail_paths(self.jail, [path])
        self.assertEqual([], self.added)

    @quiet
    def test_a_failing_dpkg_divert_is_not_fatal(self):
        cj.Jail.add_diversion.side_effect = subprocess.CalledProcessError(2, "dpkg-divert")
        cj.divert_jail_paths(self.jail, ["/usr/bin/nvidia-smi"])


class HelpersTest(JailTestCase):
    def test_mount_points_under_parses_mountinfo(self):
        mountinfo = self.tmp / "mountinfo"
        mountinfo.write_text(
            "1 2 0:1 / / rw - fs x y\n"
            "3 4 0:2 / /jail rw - fs x y\n"
            "5 6 0:3 /usr/lib/libcuda.so.1 /jail/usr/lib/libcuda.so.580 rw - fs x y\n"
            "7 8 0:4 / /jail/with\\040space rw - fs x y\n"
        )
        self.assertEqual(
            {Path("/jail/usr/lib/libcuda.so.580"), Path("/jail/with space")},
            cj.mount_points_under(Path("/jail"), mountinfo),
        )

    def test_diversions_database_parsing(self):
        self.jail.path("var/lib/dpkg/diversions").write_text("/a\n/b\n:\n/c\n/d\nsome-package\n")
        self.assertEqual({("/a", "/b"), ("/c", "/d")}, self.jail.diversions())

    def test_lock_reports_busy_without_waiting(self):
        with self.jail.lock("test", timeout=0) as acquired:
            self.assertTrue(acquired)
            with self.jail.lock("test", timeout=0) as again:
                self.assertFalse(again)
        with self.jail.lock("test", timeout=0) as acquired:
            self.assertTrue(acquired)

    def test_lock_wait_gives_up_at_the_deadline_and_restores_the_alarm_handler(self):
        previous = signal.getsignal(signal.SIGALRM)
        with self.jail.lock("test", timeout=0):
            started = time.monotonic()
            with self.jail.lock("test", timeout=0.3) as again:
                self.assertFalse(again)
            self.assertGreaterEqual(time.monotonic() - started, 0.3)
        self.assertEqual(previous, signal.getsignal(signal.SIGALRM))
        self.assertEqual((0.0, 0.0), signal.getitimer(signal.ITIMER_REAL))

    def test_noticeable_lock_waits_are_logged(self):
        holder = self.jail.path("etc/complement_jail_test.lock").open("a")
        fcntl.flock(holder, fcntl.LOCK_EX)
        threading.Timer(1.2, lambda: fcntl.flock(holder, fcntl.LOCK_UN)).start()
        with redirect_stdout(io.StringIO()) as out:
            with self.jail.lock("test", timeout=5) as acquired:
                self.assertTrue(acquired)
        holder.close()
        self.assertRegex(out.getvalue(), r"\[lock\] acquired the test lock after waiting 1\.[0-9]s")

    def test_mount_namespace_reports_errors_instead_of_raising(self):
        self.assertIn("No such file", cj.mount_namespace(cj.Jail(self.tmp / "nowhere", ARCH)))

    def test_config_from_args_and_environment(self):
        config = cj.Config.from_args(["-j", "/jail", "-u", "/upper", "-w", "--arch", ARCH], {"NODESET_GPU_ENABLED": "true", "SOPERATOR_DOCKER_ENABLED": "false"})
        self.assertEqual(
            cj.Config(Path("/jail"), Path("/upper"), True, True, False, False, "compute,utility", ARCH),
            config,
        )


class PhasesTest(JailTestCase):
    def setUp(self):
        super().setUp()
        self.mounts = patch.object(cj, "bind_mount").start()
        self.addCleanup(self.mounts.stop)

    def test_node_roles(self):
        def config(**flags):
            return cj.Config(Path("/j"), Path("/u"), flags.get("worker", False), flags.get("gpu", False), flags.get("cluster_gpu", False), True, "", ARCH)

        self.assertTrue(config(worker=True, gpu=True, cluster_gpu=True).provisions_gpu_libs)
        self.assertFalse(config(worker=True, gpu=True, cluster_gpu=True).covers_gpu_placeholders)
        self.assertTrue(config(worker=True, cluster_gpu=True).covers_gpu_placeholders, "CPU worker in a GPU cluster")
        self.assertTrue(config(cluster_gpu=True).covers_gpu_placeholders, "login node in a GPU cluster")
        self.assertFalse(config(worker=True).covers_gpu_placeholders, "CPU cluster")
        self.assertTrue(config(worker=True, gpu=True, cluster_gpu=True).refreshes_linker_cache)
        self.assertTrue(config(worker=True).refreshes_linker_cache, "every worker of a CPU cluster")
        self.assertFalse(config(worker=True, cluster_gpu=True).refreshes_linker_cache, "CPU worker in a GPU cluster")
        self.assertFalse(config(cluster_gpu=True).refreshes_linker_cache, "login node")

    @quiet
    def test_submounts_below_the_upper_dir_are_bound_at_the_same_place(self):
        upper = self.tmp / "upper"
        (upper / "home").mkdir(parents=True)
        (upper / "etc").mkdir()
        (upper / "etc/custom.conf").write_text("x")
        listing = "\n".join(f'TARGET="{path}"' for path in (upper, upper / "home", upper / "etc/custom.conf", upper / "missing", "/elsewhere"))
        with patch.object(cj, "output", return_value=listing + "\n"):
            cj.mount_jail_submounts(self.jail, upper)
        self.assertEqual(
            [
                ((upper / "home", self.jail.path("home")), {"recursive": True, "create": "dir"}),
                ((upper / "etc/custom.conf", self.jail.path("etc/custom.conf")), {"create": "file"}),
            ],
            [(call.args, call.kwargs) for call in self.mounts.call_args_list],
        )

    @quiet
    def test_zero_submounts_is_fine(self):
        with patch.object(cj, "output", return_value=""):
            cj.mount_jail_submounts(self.jail, self.tmp / "upper")
        self.mounts.assert_not_called()

    @quiet
    def test_only_small_versioned_library_files_get_libdummy(self):
        placeholder = self.jail_file(f"usr/lib/{ARCH}-linux-gnu/libcuda.so.{VERSION}", cj.PLACEHOLDER_MARKER)
        legacy_empty = self.jail_file(f"usr/lib/{ARCH}-linux-gnu/libnvidia-ml.so.{VERSION}", "")
        self.jail_file(f"usr/lib/{ARCH}-linux-gnu/libz.so.1.3")
        self.jail_file(f"usr/lib/{ARCH}-linux-gnu/libc_nonshared.a", "a" * 10)
        self.jail_file(f"usr/lib/{ARCH}-linux-gnu/libncurses.so", "INPUT(libncurses.so.6)")
        (self.jail.lib_dir / "libcuda.so.1").symlink_to(placeholder.name)
        self.jail_file(cj.GPU_LIBS_FLAG, "")

        cj.cover_gpu_placeholders_with_libdummy(self.jail)

        self.assertEqual(
            [(f"/lib/{ARCH}-linux-gnu/libdummy.so", placeholder), (f"/lib/{ARCH}-linux-gnu/libdummy.so", legacy_empty)],
            [call.args for call in self.mounts.call_args_list],
        )

    @quiet
    def test_cpu_worker_docker_config_drops_the_nvidia_default_runtime(self):
        source = self.tmp / "daemon.json"
        source.write_text('{"default-runtime": "nvidia", "runtimes": {"nvidia": {"path": "nvidia-container-runtime"}}}')
        copy = self.tmp / "run/daemon.json"
        with patch.object(cj, "DOCKER_DAEMON_CONFIG", source), patch.object(cj, "DOCKER_DAEMON_CONFIG_FOR_CPU", copy):
            cj.configure_docker(self.jail, cj.Config(Path("/j"), Path("/u"), True, False, True, True, "", ARCH))
        self.assertEqual({"default-runtime": "runc", "runtimes": {"nvidia": {"path": "nvidia-container-runtime"}}}, json.loads(copy.read_text()))
        self.mounts.assert_called_once_with(copy, self.jail.path("etc/docker/daemon.json"), create="file")

    @quiet
    def test_gpu_worker_docker_config_is_bound_as_is(self):
        cj.configure_docker(self.jail, cj.Config(Path("/j"), Path("/u"), True, True, True, True, "", ARCH))
        self.mounts.assert_called_once_with(cj.DOCKER_DAEMON_CONFIG, self.jail.path("etc/docker/daemon.json"), create="file")

    @quiet
    def test_disabled_docker_masks_existing_binaries_with_the_stub(self):
        self.jail_file("usr/bin/docker", "")
        stub = self.tmp / "stub.sh"
        with patch.object(cj, "DOCKER_DISABLED_STUB", stub):
            cj.configure_docker(self.jail, cj.Config(Path("/j"), Path("/u"), True, False, False, False, "", ARCH))
        self.assertIn(cj.DOCKER_DISABLED_MESSAGE, stub.read_text())
        self.mounts.assert_called_once_with(stub, self.jail.path("usr/bin/docker"))


if __name__ == "__main__":
    unittest.main()
