import importlib.util
import json
import os
import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest import mock


CHECK_RUNNER_PATH = Path(__file__).with_name("check_runner.py")


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


def load_check_runner():
    required_env = {
        "SLURMD_NODENAME": "worker-1",
        "CHECKS_OUTPUTS_BASE_DIR": "/opt/soperator-outputs",
        "CHECKS_CONTEXT": "hc_program",
        "CHECKS_CONFIG": "/opt/slurm_scripts/checks.json",
    }
    with mock.patch.dict(os.environ, required_env), mock.patch("logging.basicConfig"):
        spec = importlib.util.spec_from_file_location(
            "check_runner_under_test", CHECK_RUNNER_PATH
        )
        module = importlib.util.module_from_spec(spec)
        assert spec.loader is not None
        spec.loader.exec_module(module)
        return module


check_runner = load_check_runner()


class NodeRealMemoryMetadataTest(unittest.TestCase):
    def setUp(self):
        check_runner.get_node_real_memory_bytes.cache_clear()

    def test_reads_real_memory_from_local_metadata_without_node_rpc(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            metadata_file = Path(tmpdir) / "node_metadata.env"
            metadata_file.write_text(
                "IGNORED=value\n"
                "SOPERATOR_NODE_REAL_MEMORY_BYTES=999292928\n",
                encoding="utf-8",
            )

            with (
                mock.patch.object(
                    check_runner, "SOPERATOR_NODE_METADATA_FILE", str(metadata_file)
                ),
                mock.patch.object(check_runner, "get_node_info") as get_node_info,
            ):
                result = check_runner.get_node_real_memory_bytes()

            self.assertEqual(999292928, result)
            get_node_info.assert_not_called()

    def test_exports_local_real_memory_for_checks(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            metadata_file = Path(tmpdir) / "node_metadata.env"
            metadata_file.write_text(
                "SOPERATOR_NODE_REAL_MEMORY_BYTES=999292928\n",
                encoding="utf-8",
            )
            check = check_runner.Check(need_env=["CHECKS_NODE_REAL_MEM_BYTES"])

            with (
                mock.patch.object(
                    check_runner, "SOPERATOR_NODE_METADATA_FILE", str(metadata_file)
                ),
                mock.patch.object(check_runner, "get_node_info") as get_node_info,
                mock.patch.dict(os.environ, {}, clear=False),
            ):
                check_runner.export_needed_env(check)
                exported_value = os.environ["CHECKS_NODE_REAL_MEM_BYTES"]

            self.assertEqual("999292928", exported_value)
            get_node_info.assert_not_called()

    def test_falls_back_to_slurm_when_metadata_is_missing(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            missing_file = Path(tmpdir) / "missing.env"
            node_info = check_runner.NodeInfo(real_memory_bytes=2147483648)

            with (
                mock.patch.object(
                    check_runner, "SOPERATOR_NODE_METADATA_FILE", str(missing_file)
                ),
                mock.patch.object(
                    check_runner, "get_node_info", return_value=node_info
                ) as get_node_info,
            ):
                result = check_runner.get_node_real_memory_bytes()

            self.assertEqual(2147483648, result)
            get_node_info.assert_called_once_with()

    def test_falls_back_to_slurm_when_metadata_is_invalid(self):
        with tempfile.TemporaryDirectory() as tmpdir:
            metadata_file = Path(tmpdir) / "node_metadata.env"
            metadata_file.write_text(
                "SOPERATOR_NODE_REAL_MEMORY_BYTES=invalid\n", encoding="utf-8"
            )
            node_info = check_runner.NodeInfo(real_memory_bytes=1073741824)

            with (
                mock.patch.object(
                    check_runner, "SOPERATOR_NODE_METADATA_FILE", str(metadata_file)
                ),
                mock.patch.object(
                    check_runner, "get_node_info", return_value=node_info
                ) as get_node_info,
            ):
                result = check_runner.get_node_real_memory_bytes()

            self.assertEqual(1073741824, result)
            get_node_info.assert_called_once_with()


class PartialCPUJobsTest(unittest.TestCase):
    def setUp(self):
        patcher = mock.patch.multiple(check_runner,
            SLURMD_NODENAME="worker-1", SLURM_JOB_GPUS="", CHECKS_CONTEXT="epilog",
            SLURM_JOB_CPUS_PER_NODE="8", SLURM_JOB_NODELIST="worker-1",
        )
        patcher.start()
        self.addCleanup(patcher.stop)
        for name in ("get_job_alloc_cpus", "get_node_info", "get_platform_tags"):
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
            real_memory_bytes=1024 * 1024 * 1024, effective_cpus=64,
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
                with mock.patch.object(check_runner, "get_platform_tags", return_value=["8xGPU"]):
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

    def test_builtin_cleanup_runs_only_for_full_cpu_or_gpu_allocations(self):
        config_path = CHECK_RUNNER_PATH.with_name("drop_posix_shmem.sh.json")
        check = check_runner.Check(**json.loads(config_path.read_text()))
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
                    mock.patch.object(check_runner, "get_platform_tags", return_value=["8xGPU"]),
                ):
                    check_runner.get_job_alloc_cpus.cache_clear()
                    expected = [check] if should_run else []
                    self.assertEqual(check_runner.filter_applicable_checks([check]), expected)

        with mock.patch.object(check_runner, "CHECKS_CONTEXT", "hc_program"):
            self.assertEqual(check_runner.filter_applicable_checks([check]), [])
        self.subprocess_run.assert_called_once()


if __name__ == "__main__":
    unittest.main(verbosity=2)
