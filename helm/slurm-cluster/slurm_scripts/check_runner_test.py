import importlib.util
import json
import os
import runpy
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest import mock


CHECK_RUNNER_PATH = Path(__file__).with_name("check_runner.py")
DEFAULT_METADATA = (
    "NODESET_GPU_ENABLED=false\nSOPERATOR_NODE_PLATFORM_TAG=CPU\nSOPERATOR_NODE_PLATFORM_TAGS=CPU\n"
    "SOPERATOR_NODE_REAL_MEMORY_BYTES=1024\n"
)



class CheckRunnerOnOkContextTest(unittest.TestCase):
    def load_runner(self, context: str, tmpdir: str):
        env = {
            "SLURMD_NODENAME": "worker-1",
            "CHECKS_OUTPUTS_BASE_DIR": tmpdir,
            "CHECKS_CONTEXT": context,
            "CHECKS_CONFIG": str(Path(tmpdir) / "checks.json"),
            "CHECKS_RUNNER_OUTPUT": "/dev/null",
        }
        patcher = mock.patch.dict(os.environ, env, clear=False)
        patcher.start()
        self.addCleanup(patcher.stop)

        module_name = f"check_runner_under_test_{context}_{id(self)}"
        spec = importlib.util.spec_from_file_location(module_name, CHECK_RUNNER_PATH)
        runner = importlib.util.module_from_spec(spec)
        with mock.patch("builtins.open", mock.mock_open(read_data=DEFAULT_METADATA)):
            spec.loader.exec_module(runner)
        return runner

    def test_undrain_on_ok_is_ignored_outside_hc_program(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            runner = self.load_runner("prolog", tmpdir)
            check = runner.Check(
                name="boot_disk_full",
                command="/usr/bin/true",
                on_ok="undrain",
                reason_base="[user_problem] $name",
                log="check.out",
            )

            def get_node_info():
                raise AssertionError("prolog on_ok=undrain must not read Slurm node info")

            def undrain_node():
                raise AssertionError("prolog on_ok=undrain must not undrain Slurm nodes")

            runner.get_node_info = get_node_info
            runner.undrain_node = undrain_node

            runner.run_check(check, in_jail=True)

    def test_undrain_on_ok_still_runs_in_hc_program(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            runner = self.load_runner("hc_program", tmpdir)
            check = runner.Check(
                name="boot_disk_full",
                command="/usr/bin/true",
                on_ok="undrain",
                reason_base="[user_problem] $name",
                log="check.out",
            )
            calls = []

            runner.get_node_info = lambda: runner.NodeInfo(
                state_flags=["DRAIN"],
                reason="[user_problem] boot_disk_full: disk ok [hc_program]",
            )
            runner.undrain_node = lambda: calls.append("undrain")

            runner.run_check(check, in_jail=True)

            self.assertEqual(["undrain"], calls)

    def test_uncomment_on_ok_is_ignored_outside_hc_program(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            runner = self.load_runner("epilog", tmpdir)
            check = runner.Check(
                name="some_check",
                command="/usr/bin/true",
                on_ok="uncomment",
                reason_base="[node_problem] $name",
                log="check.out",
            )

            def get_node_info():
                raise AssertionError("epilog on_ok=uncomment must not read Slurm node info")

            def uncomment_node():
                raise AssertionError("epilog on_ok=uncomment must not uncomment Slurm nodes")

            runner.get_node_info = get_node_info
            runner.uncomment_node = uncomment_node

            runner.run_check(check, in_jail=True)

    def test_uncomment_on_ok_still_runs_in_hc_program(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            runner = self.load_runner("hc_program", tmpdir)
            check = runner.Check(
                name="some_check",
                command="/usr/bin/true",
                on_ok="uncomment",
                reason_base="[node_problem] $name",
                log="check.out",
            )
            calls = []

            runner.get_node_info = lambda: runner.NodeInfo(
                comment="[node_problem] some_check: recovered [hc_program]",
            )
            runner.uncomment_node = lambda: calls.append("uncomment")

            runner.run_check(check, in_jail=True)

            self.assertEqual(["uncomment"], calls)


def load_check_runner(metadata=DEFAULT_METADATA):
    required_env = {
        "SLURMD_NODENAME": "worker-1",
        "CHECKS_OUTPUTS_BASE_DIR": "/opt/soperator-outputs",
        "CHECKS_CONTEXT": "hc_program",
        "CHECKS_CONFIG": "/opt/slurm_scripts/checks.json",
    }
    with (
        mock.patch.dict(os.environ, required_env), mock.patch("logging.basicConfig"),
        mock.patch("builtins.open", mock.mock_open(read_data=metadata)),
    ):
        spec = importlib.util.spec_from_file_location(
            "check_runner_under_test", CHECK_RUNNER_PATH
        )
        module = importlib.util.module_from_spec(spec)
        assert spec.loader is not None
        spec.loader.exec_module(module)
        return module


check_runner = load_check_runner()


class NodeMetadataTest(unittest.TestCase):
    def test_platform_and_memory_are_read_once_at_startup(self):
        data = ("NODESET_GPU_ENABLED=true\nSOPERATOR_NODE_PLATFORM_TAG=8xH200\n"
                "SOPERATOR_NODE_PLATFORM_TAGS=8xH200,8xGPU\nSOPERATOR_NODE_REAL_MEMORY_BYTES=999292928\n")
        runner = load_check_runner(data)
        self.assertEqual("8xH200", runner.CHECKS_PLATFORM_TAG)
        self.assertEqual(["8xH200", "8xGPU"], runner.CHECKS_PLATFORM_TAGS)
        self.assertEqual(999292928, runner.CHECKS_NODE_REAL_MEM_BYTES)
        with mock.patch("builtins.open", side_effect=AssertionError("must not reread metadata")):
            check = runner.Check(platforms=["8xGPU"])
            self.assertEqual([check], runner.filter_by_platform([check]))
            with mock.patch.object(runner, "CHECKS_CONTEXT", "prolog"), mock.patch.object(runner, "SLURM_JOB_GPUS", "0"):
                self.assertEqual([], runner.filter_by_skip_for_partial_gpu_jobs([check._replace(skip_for_partial_gpu_jobs=True)]))

    def test_reader_returns_all_variables_or_value_by_key(self):
        with mock.patch("builtins.open", mock.mock_open(read_data=DEFAULT_METADATA + "EXTRA_METADATA=value=with=equals\n")):
            self.assertEqual("value=with=equals", check_runner.read_node_metadata("EXTRA_METADATA"))
            self.assertEqual("CPU", check_runner.read_node_metadata()[check_runner.SOPERATOR_NODE_PLATFORM_TAG])
            with self.assertRaises(KeyError):
                check_runner.read_node_metadata("MISSING")

    def test_gpu_checks_remain_selected_when_nvidia_smi_cannot_run(self):
        for tag in ("8xH100", "8xH200", "8xB200", "8xB300", "4xGB300"):
            data = (f"NODESET_GPU_ENABLED=true\nSOPERATOR_NODE_PLATFORM_TAG={tag}\n"
                    f"SOPERATOR_NODE_PLATFORM_TAGS={tag},{tag.split('x')[0]}xGPU\nSOPERATOR_NODE_REAL_MEMORY_BYTES=1024\n")
            for failure in (FileNotFoundError("nvidia-smi"), subprocess.CalledProcessError(1, "nvidia-smi")):
                with self.subTest(tag=tag, failure=type(failure).__name__), mock.patch("subprocess.run", side_effect=failure) as run:
                    runner = load_check_runner(data)
                    checks = [runner.Check(**json.loads(CHECK_RUNNER_PATH.with_name(name).read_text())) for name in
                              ("gpu_health_check.py.json", "alloc_gpus_busy.drain.sh.json")]
                    self.assertEqual(checks, runner.filter_by_platform(checks))
                    run.assert_not_called()

    def test_any_gpu_model_from_gres_type_is_accepted(self):
        data = ("NODESET_GPU_ENABLED=true\nSOPERATOR_NODE_PLATFORM_TAG=4xA100\n"
                "SOPERATOR_NODE_PLATFORM_TAGS=4xA100,4xGPU\nSOPERATOR_NODE_REAL_MEMORY_BYTES=1024\n")
        runner = load_check_runner(data)
        model, generic, other = (runner.Check(platforms=[tag]) for tag in ("4xA100", "4xGPU", "8xH100"))
        self.assertEqual([model, generic], runner.filter_by_platform([model, generic, other]))

    def test_cpu_worker_selects_only_cpu_and_any_platforms(self):
        runner = load_check_runner()
        cpu = runner.Check(platforms=["CPU"])
        any_platform = runner.Check()
        gpu = runner.Check(platforms=["8xGPU"])
        self.assertEqual([cpu, any_platform], runner.filter_by_platform([cpu, any_platform, gpu]))

    def test_builtin_checks_no_longer_request_metadata_in_need_env(self):
        metadata_keys = {"CHECKS_PLATFORM_TAG", "CHECKS_PLATFORM_TAGS", "CHECKS_NODE_REAL_MEM_BYTES"}
        for path in CHECK_RUNNER_PATH.parent.glob("*.json"):
            with self.subTest(path=path.name):
                self.assertFalse(metadata_keys.intersection(json.loads(path.read_text()).get("need_env", [])))


class MetadataStartupTest(unittest.TestCase):
    def run_runner(self, tmpdir, context, metadata, check, mock_commands=True):
        metadata_path = Path(tmpdir) / "node_metadata.env"
        config_path = Path(tmpdir) / "checks.json"
        config_path.write_text(json.dumps([check]))
        if isinstance(metadata, str):
            metadata_path.write_text(metadata)
        elif isinstance(metadata, bytes):
            metadata_path.write_bytes(metadata)
        real_open = open

        def open_with_metadata(path, *args, **kwargs):
            if path == check_runner.SOPERATOR_NODE_METADATA_FILE:
                if isinstance(metadata, OSError):
                    raise metadata
                path = metadata_path
            return real_open(path, *args, **kwargs)

        env = {
            "SLURMD_NODENAME": "worker-1", "CHECKS_CONTEXT": context,
            "CHECKS_CONFIG": str(config_path), "CHECKS_OUTPUTS_BASE_DIR": tmpdir,
            "SLURM_JOB_COMMENT": "", "CHECKS_PLATFORM_TAG": "stale",
            "CHECKS_PLATFORM_TAGS": "stale", "CHECKS_NODE_REAL_MEM_BYTES": "999",
        }
        with (
            mock.patch.dict(os.environ, env),
            mock.patch("builtins.open", side_effect=open_with_metadata) as open_file,
            mock.patch("logging.basicConfig"), mock.patch("os.chdir"), mock.patch("os.chroot", create=True),
            mock.patch("subprocess.run", wraps=subprocess.run if not mock_commands else None) as run,
            self.assertRaises(SystemExit) as exited,
        ):
            try:
                runpy.run_path(str(CHECK_RUNNER_PATH), run_name="__main__")
            finally:
                self.assertEqual(1, sum(call.args[0] == check_runner.SOPERATOR_NODE_METADATA_FILE
                                        for call in open_file.call_args_list))
                if mock_commands:
                    run.assert_not_called()
        self.assertEqual(0, exited.exception.code)

    def test_metadata_errors_log_and_exit_zero_even_for_checks_without_need_env(self):
        values = dict(line.split("=", 1) for line in DEFAULT_METADATA.splitlines())
        cases = [None, PermissionError("metadata is unreadable"), b"\xff", "MALFORMED\n",
                 DEFAULT_METADATA + "SOPERATOR_NODE_PLATFORM_TAG=CPU\n", DEFAULT_METADATA + "BAD-KEY=value\n"]
        for key in values:
            cases.append("".join(f"{name}={value}\n" for name, value in values.items() if name != key))
        for key, value in ((check_runner.SOPERATOR_NODE_REAL_MEMORY_BYTES, "0"), (check_runner.SOPERATOR_NODE_REAL_MEMORY_BYTES, "bad"),
                           (check_runner.SOPERATOR_NODE_REAL_MEMORY_BYTES, "-1"), (check_runner.SOPERATOR_NODE_PLATFORM_TAG, "8xH200"),
                           (check_runner.SOPERATOR_NODE_PLATFORM_TAGS, ""), ("NODESET_GPU_ENABLED", "invalid"),
                           ("NODESET_GPU_ENABLED", "true")):
            cases.append("".join(f"{name}={value if name == key else original}\n" for name, original in values.items()))
        for context in ("prolog", "epilog", "hc_program"):
            for metadata in cases:
                with self.subTest(context=context, metadata=metadata), tempfile.TemporaryDirectory() as tmpdir:
                    with self.assertLogs(level="ERROR") as logs:
                        self.run_runner(tmpdir, context, metadata, {"name": "metadata-regression", "need_env": []})
                    self.assertTrue(any("skipping checks" in line for line in logs.output), logs.output)

    def test_child_check_inherits_all_metadata_without_need_env(self):
        for tag, tags, enabled in (("CPU", "CPU", "false"), ("8xH200", "8xH200,8xGPU", "true"), ("1xGPU", "1xGPU", "true")):
            with self.subTest(tag=tag), tempfile.TemporaryDirectory() as tmpdir:
                writer = CHECK_RUNNER_PATH.parents[3] / "images/worker/write_soperator_metadata.sh"
                metadata_path = Path(tmpdir) / "writer.env"
                env = dict(PATH=os.environ["PATH"], NODESET_GPU_ENABLED=enabled,
                           SOPERATOR_NODE_REAL_MEMORY_BYTES="1024",
                           SOPERATOR_NODE_PLATFORM_TAG=tag, SOPERATOR_NODE_PLATFORM_TAGS=tags,
                           SOPERATOR_NODE_CPUS="128")
                result = subprocess.run(["bash", str(writer), str(metadata_path)], env=env, capture_output=True, text=True)
                self.assertEqual(0, result.returncode, result.stderr)
                metadata = metadata_path.read_text() + "EXTRA_METADATA=value=with=equals\n"
                check = {
                    "name": "metadata-inheritance", "need_env": [], "run_in_jail": True, "log": "child.out",
                    "command": "printf '%s\\n' \"$CHECKS_PLATFORM_TAG\" \"$CHECKS_PLATFORM_TAGS\" \"$CHECKS_NODE_REAL_MEM_BYTES\" \"$EXTRA_METADATA\" \"$SOPERATOR_NODE_CPUS\"",
                }
                self.run_runner(tmpdir, "hc_program", metadata, check, mock_commands=False)
                self.assertEqual(f"{tag}\n{tags}\n1024\nvalue=with=equals\n128\n", (Path(tmpdir) / "child.out").read_text())


class PartialCPUJobsTest(unittest.TestCase):
    def setUp(self):
        patcher = mock.patch.multiple(check_runner,
            SLURMD_NODENAME="worker-1", SLURM_JOB_GPUS="", CHECKS_CONTEXT="epilog",
            SLURM_JOB_CPUS_PER_NODE="8", SLURM_JOB_NODELIST="worker-1",
        )
        patcher.start()
        self.addCleanup(patcher.stop)
        for name in ("get_job_alloc_cpus", "get_node_info"):
            getter = getattr(check_runner, name)
            getter.cache_clear()
            self.addCleanup(getter.cache_clear)
        patcher = mock.patch.object(check_runner.subprocess, "run", side_effect=self.scontrol)
        self.subprocess_run = patcher.start()
        self.addCleanup(patcher.stop)
        self.addCleanup(self.assert_no_job_queries)
        self.node = {
            "name": "worker-1", "cpus": 64, "effective_cpus": 64,
            "state": ["DRAIN"], "reason": "test reason", "comment": "test comment",
            "real_memory": 1024,
        }
        self.hosts = {"worker-[0-2]": "worker-0\nworker-1\nworker-2\n"}
        self.guarded = check_runner.Check(name="cleanup", skip_for_partial_cpu_jobs=True)
        self.unguarded = check_runner.Check(name="other")
        self.checks = [self.guarded, self.unguarded]

    def scontrol(self, command, **kwargs):
        if command == ["scontrol", "show", "node", "worker-1", "--json"]:
            output = json.dumps({"nodes": [self.node]})
        else:
            self.assertEqual(command[:3], ["scontrol", "show", "hostnames"])
            output = self.hosts[command[3]]
        return subprocess.CompletedProcess(command, 0, stdout=output, stderr="")

    def assert_no_job_queries(self):
        for call in self.subprocess_run.call_args_list:
            self.assertIn(call.args[0][:3], (["scontrol", "show", "hostnames"], ["scontrol", "show", "node"]))

    def assert_cpu_data_unavailable(self):
        check_runner.get_job_alloc_cpus.cache_clear()
        check_runner.get_node_info.cache_clear()
        with self.assertLogs(level="WARNING") as logs:
            self.assertEqual(check_runner.filter_applicable_checks(self.checks), [self.unguarded])
        self.assertTrue(any("CPU allocation data is unavailable or invalid" in line for line in logs.output))

    def test_partial_and_full_cpu_jobs_in_prolog_and_epilog(self):
        for context in ("prolog", "epilog"):
            for cpus, expected in (("8", [self.unguarded]), ("64", self.checks), ("128", self.checks)):
                with (
                    self.subTest(context=context, cpus=cpus),
                    mock.patch.multiple(check_runner, CHECKS_CONTEXT=context, SLURM_JOB_CPUS_PER_NODE=cpus),
                ):
                    check_runner.get_job_alloc_cpus.cache_clear()
                    self.assertEqual(check_runner.filter_applicable_checks(self.checks), expected)
        self.subprocess_run.assert_called_once()

    def test_repeated_cpu_counts_select_the_current_node(self):
        for index, cpus in ((0, 64), (1, 64), (2, 32)):
            with self.subTest(index=index), mock.patch.multiple(check_runner,
                SLURMD_NODENAME=f"worker-{index}", SLURM_JOB_CPUS_PER_NODE="64(x2),32",
                SLURM_JOB_NODELIST="worker-[0-2]",
            ):
                check_runner.get_job_alloc_cpus.cache_clear()
                self.assertEqual(check_runner.get_job_alloc_cpus(), cpus)

    def test_comma_separated_node_names_preserve_allocation_order(self):
        with mock.patch.multiple(check_runner,
            SLURM_JOB_CPUS_PER_NODE="16,64,8", SLURM_JOB_NODELIST="worker-2,worker-1,worker-0",
        ):
            self.assertEqual(check_runner.filter_applicable_checks(self.checks), self.checks)
        self.subprocess_run.assert_called_once()

    def test_invalid_or_missing_cpu_environment_skips_only_guarded_checks(self):
        cases = [
            ("", "worker-1"), ("0", "worker-1"), ("-1", "worker-1"), ("64.0", "worker-1"),
            ("64(x0)", "worker-1"), ("64(x-1)", "worker-1"), ("64,", "worker-1"),
            ("64", ""), ("64", "worker-0"), ("64(x2)", "worker-1"),
            ("64", "worker-0,worker-1"), ("64(x2)", "worker-1,worker-1"),
        ]
        for cpus, nodes in cases:
            with self.subTest(cpus=cpus, nodes=nodes), mock.patch.multiple(check_runner,
                SLURM_JOB_CPUS_PER_NODE=cpus, SLURM_JOB_NODELIST=nodes,
            ):
                self.assert_cpu_data_unavailable()

    def test_hostlist_expansion_failures_skip_only_guarded_checks(self):
        for failure in (
            FileNotFoundError("scontrol"),
            subprocess.CalledProcessError(1, "scontrol"),
            subprocess.TimeoutExpired("scontrol", 10),
        ):
            with self.subTest(failure=type(failure).__name__), mock.patch.multiple(check_runner,
                SLURM_JOB_CPUS_PER_NODE="64(x3)", SLURM_JOB_NODELIST="worker-[0-2]",
            ):
                def scontrol(command, **kwargs):
                    if command[:3] == ["scontrol", "show", "hostnames"]:
                        raise failure
                    return self.scontrol(command, **kwargs)

                self.subprocess_run.side_effect = scontrol
                self.assert_cpu_data_unavailable()

    def test_feature_expressions_do_not_trigger_hostlist_rpcs(self):
        for nodes in ("{feature}", "worker-[0-2]{feature}"):
            with self.subTest(nodes=nodes), mock.patch.multiple(check_runner,
                SLURM_JOB_CPUS_PER_NODE="64(x3)", SLURM_JOB_NODELIST=nodes,
            ):
                self.assert_cpu_data_unavailable()
        for call in self.subprocess_run.call_args_list:
            self.assertEqual(call.args[0][:3], ["scontrol", "show", "node"])

    def test_uses_effective_cpus_from_node_info(self):
        self.node["effective_cpus"] = 60
        with mock.patch.object(check_runner, "SLURM_JOB_CPUS_PER_NODE", "60"):
            self.assertEqual(check_runner.filter_applicable_checks(self.checks), self.checks)
        self.subprocess_run.assert_called_once()

    def test_missing_or_invalid_effective_cpus_skip_only_guarded_checks(self):
        for cpus in (None, 0, -1, "64", True, 64.0):
            with self.subTest(cpus=cpus):
                self.node["effective_cpus"] = cpus
                self.assert_cpu_data_unavailable()
        del self.node["effective_cpus"]
        self.assert_cpu_data_unavailable()

    def test_node_query_errors_skip_only_guarded_checks(self):
        for failure in (FileNotFoundError("scontrol"), subprocess.CalledProcessError(1, "scontrol")):
            with self.subTest(failure=type(failure).__name__):
                self.subprocess_run.side_effect = failure
                self.assert_cpu_data_unavailable()

    def test_invalid_node_responses_skip_only_guarded_checks(self):
        for output in ("invalid JSON", "{}", '{"nodes": []}'):
            with self.subTest(output=output):
                self.subprocess_run.side_effect = None
                self.subprocess_run.return_value = subprocess.CompletedProcess("scontrol", 0, stdout=output, stderr="")
                self.assert_cpu_data_unavailable()

    def test_node_info_is_shared_with_other_filters_and_preserves_metadata(self):
        check = self.guarded._replace(node_states=["drain"])
        with mock.patch.object(check_runner, "SLURM_JOB_CPUS_PER_NODE", "64"):
            self.assertEqual(check_runner.filter_applicable_checks([check]), [check])
        self.assertEqual(check_runner.get_node_info(), check_runner.NodeInfo(
            state_flags=["DRAIN"], reason="test reason", comment="test comment",
            effective_cpus=64,
        ))
        self.subprocess_run.assert_called_once()

    def test_disabled_flag_and_non_job_context_do_not_query_node_info(self):
        self.assertEqual(check_runner.filter_applicable_checks([self.unguarded]), [self.unguarded])
        with mock.patch.object(check_runner, "CHECKS_CONTEXT", "hc_program"):
            self.assertEqual(check_runner.filter_applicable_checks(self.checks), self.checks)
        self.assertEqual(check_runner.get_node_info.cache_info().misses, 0)
        self.subprocess_run.assert_not_called()

    def test_other_filters_can_skip_checks_before_querying_node_info(self):
        for check in (self.guarded._replace(contexts=["prolog"]), self.guarded._replace(skip_for_cpu_jobs=True)):
            with self.subTest(check=check):
                self.assertEqual(check_runner.filter_applicable_checks([check]), [])
        self.assertEqual(check_runner.get_node_info.cache_info().misses, 0)
        self.subprocess_run.assert_not_called()

    def test_gpu_jobs_bypass_cpu_filter_and_still_use_gpu_filter(self):
        for gpus in ("0", "0,1,2,3,4,5,6,7"):
            with self.subTest(gpus=gpus), mock.patch.object(check_runner, "SLURM_JOB_GPUS", gpus):
                self.assertEqual(check_runner.filter_applicable_checks(self.checks), self.checks)
                check = self.guarded._replace(skip_for_partial_gpu_jobs=True)
                with mock.patch.object(check_runner, "CHECKS_PLATFORM_TAGS", ["8xGPU"]):
                    expected = [] if gpus == "0" else [check]
                    self.assertEqual(check_runner.filter_applicable_checks([check]), expected)
        self.assertEqual(check_runner.get_node_info.cache_info().misses, 0)
        self.subprocess_run.assert_not_called()

    def test_hostlist_expansion_is_cached_with_job_cpu_allocation(self):
        with mock.patch.multiple(check_runner,
            SLURM_JOB_CPUS_PER_NODE="64(x3)", SLURM_JOB_NODELIST="worker-[0-2]",
        ):
            for _ in range(2):
                self.assertEqual(check_runner.filter_applicable_checks(self.checks), self.checks)
        self.assertEqual(self.subprocess_run.call_count, 2)

    def test_configs_without_partial_cpu_flag_default_to_disabled(self):
        for path in CHECK_RUNNER_PATH.parent.glob("*.json"):
            with self.subTest(config=path.name):
                config = json.loads(path.read_text())
                config.pop("skip_for_partial_cpu_jobs", None)
                self.assertFalse(check_runner.Check(**config).skip_for_partial_cpu_jobs)

    def test_check_runs_only_for_full_cpu_or_gpu_allocations(self):
        check = self.guarded._replace(
            skip_for_partial_gpu_jobs=True, contexts=["prolog", "epilog"],
        )
        cases = (
            ("", "8", False),
            ("", "64", True),
            ("", "", False),
            ("0", "64", False),
            ("0,1,2,3,4,5,6,7", "8", True),
        )
        for context in ("prolog", "epilog"):
            for gpus, cpus, should_run in cases:
                with (
                    self.subTest(context=context, gpus=gpus, cpus=cpus),
                    mock.patch.multiple(check_runner,
                        CHECKS_CONTEXT=context, SLURM_JOB_GPUS=gpus, SLURM_JOB_CPUS_PER_NODE=cpus,
                    ),
                    mock.patch.object(check_runner, "CHECKS_PLATFORM_TAGS", ["8xGPU"]),
                ):
                    check_runner.get_job_alloc_cpus.cache_clear()
                    expected = [check] if should_run else []
                    self.assertEqual(check_runner.filter_applicable_checks([check]), expected)

        with mock.patch.object(check_runner, "CHECKS_CONTEXT", "hc_program"):
            self.assertEqual(check_runner.filter_applicable_checks([check]), [])
        self.subprocess_run.assert_called_once()


if __name__ == "__main__":
    unittest.main(verbosity=2)
