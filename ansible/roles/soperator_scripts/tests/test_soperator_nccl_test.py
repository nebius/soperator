import argparse
import importlib.machinery
import importlib.util
import subprocess
import sys
import unittest
from pathlib import Path
from unittest import mock


SCRIPT_PATH = Path(__file__).parents[1] / "files" / "soperator-nccl-test"
LOADER = importlib.machinery.SourceFileLoader("soperator_nccl_test", str(SCRIPT_PATH))
SPEC = importlib.util.spec_from_loader(LOADER.name, LOADER)
nccl_test = importlib.util.module_from_spec(SPEC)
sys.modules[SPEC.name] = nccl_test
LOADER.exec_module(nccl_test)


def args(**overrides):
  values = {
    "nodes": 2,
    "partition": None,
    "nodelist": None,
    "immediate": None,
    "segment": None,
    "min_message_size": "512M",
    "max_message_size": "16G",
    "message_step_factor": "2",
    "time_limit": "30:00",
  }
  values.update(overrides)
  return argparse.Namespace(**values)


class ArgumentTests(unittest.TestCase):
  def test_nodes_is_required(self):
    with self.assertRaises(SystemExit):
      nccl_test.parse_args([])

  def test_defaults(self):
    parsed = nccl_test.parse_args(["--nodes", "2"])

    self.assertEqual(parsed.nodes, 2)
    self.assertIsNone(parsed.partition)
    self.assertIsNone(parsed.nodelist)
    self.assertIsNone(parsed.immediate)
    self.assertIsNone(parsed.segment)
    self.assertEqual(parsed.min_message_size, "512M")
    self.assertEqual(parsed.max_message_size, "16G")
    self.assertEqual(parsed.message_step_factor, "2")
    self.assertEqual(parsed.time_limit, "30:00")

  def test_rejects_invalid_values(self):
    invalid_arguments = (
      ["--nodes", "0"],
      ["--nodes", "2", "--segment", "0"],
      ["--nodes", "2", "--nodelist", "worker-0,"],
      ["--nodes", "2", "--nodelist", "worker-0,worker-0"],
      ["--nodes", "2", "--immediate", "0"],
      ["--nodes", "2", "--min-message-size", "invalid"],
      ["--nodes", "2", "--message-step-factor", "1"],
      ["--nodes", "2", "--time-limit", "invalid"],
    )

    for arguments in invalid_arguments:
      with self.subTest(arguments=arguments), self.assertRaises(SystemExit):
        nccl_test.parse_args(arguments)

  def test_parses_nodelist_and_immediate(self):
    parsed = nccl_test.parse_args(
      ["--nodes", "2", "--nodelist", "worker-0,worker-1", "--immediate", "120"]
    )

    self.assertEqual(parsed.nodelist, ("worker-0", "worker-1"))
    self.assertEqual(parsed.immediate, 120)

  def test_rejects_nodelist_with_wrong_node_count(self):
    with self.assertRaisesRegex(ValueError, "contains 1 node"):
      nccl_test.validate_args(args(nodelist=("worker-0",)))


class PlatformDetectionTests(unittest.TestCase):
  def test_detects_every_supported_platform(self):
    for expected in nccl_test.PLATFORM_PROFILES:
      with self.subTest(platform=expected.name), mock.patch.object(
        nccl_test.subprocess,
        "run",
        return_value=subprocess.CompletedProcess([], 0, f"{expected.gpu_name}\n", ""),
      ):
        actual = nccl_test.detect_platform(args())
        self.assertEqual(actual, expected)

  @mock.patch.object(nccl_test.subprocess, "run")
  def test_partition_is_used_for_probe(self, run):
    run.return_value = subprocess.CompletedProcess([], 0, "NVIDIA H200\n", "")

    nccl_test.detect_platform(args(partition="main"))

    command = run.call_args.args[0]
    self.assertIn("--partition=main", command)

  @mock.patch.object(nccl_test.subprocess, "run")
  def test_partition_is_omitted_from_probe_by_default(self, run):
    run.return_value = subprocess.CompletedProcess([], 0, "NVIDIA H200\n", "")

    nccl_test.detect_platform(args())

    command = run.call_args.args[0]
    self.assertFalse(any(value.startswith("--partition=") for value in command))

  @mock.patch.object(nccl_test.subprocess, "run")
  def test_probes_every_selected_node_and_fails_fast(self, run):
    run.return_value = subprocess.CompletedProcess(
      [], 0, "NVIDIA B300\nNVIDIA B300\n", ""
    )

    profile = nccl_test.detect_platform(
      args(nodelist=("worker-0", "worker-1"), immediate=120)
    )

    self.assertEqual(profile.name, "B300")
    command = run.call_args.args[0]
    self.assertIn("--nodes=2", command)
    self.assertIn("--ntasks-per-node=1", command)
    self.assertIn("--nodelist=worker-0,worker-1", command)
    self.assertIn("--immediate=120", command)

  @mock.patch.object(nccl_test.subprocess, "run")
  def test_rejects_mixed_selected_platforms(self, run):
    run.return_value = subprocess.CompletedProcess(
      [], 0, "NVIDIA H200\nNVIDIA B300\n", ""
    )

    with self.assertRaises(SystemExit):
      nccl_test.detect_platform(args(nodelist=("worker-0", "worker-1")))

  @mock.patch.object(nccl_test.subprocess, "run")
  def test_rejects_unsupported_platform(self, run):
    run.return_value = subprocess.CompletedProcess([], 0, "Unknown GPU\n", "")

    with self.assertRaises(SystemExit):
      nccl_test.detect_platform(args())

  @mock.patch.object(nccl_test.subprocess, "run")
  def test_propagates_probe_failure(self, run):
    run.return_value = subprocess.CompletedProcess([], 7, "", "probe failed\n")

    with self.assertRaises(SystemExit) as error:
      nccl_test.detect_platform(args())

    self.assertEqual(error.exception.code, 7)


class CommandTests(unittest.TestCase):
  def setUp(self):
    self.h200 = next(
      profile for profile in nccl_test.PLATFORM_PROFILES if profile.name == "H200"
    )
    self.gb300 = next(
      profile for profile in nccl_test.PLATFORM_PROFILES if profile.name == "GB300"
    )

  def test_builds_h200_command_with_defaults(self):
    command = nccl_test.build_nccl_command(args(), self.h200)

    self.assertEqual(
      command,
      [
        "srun",
        "--job-name=nccl_all_reduce",
        "--time=30:00",
        "--nodes=2",
        "--gpus-per-node=8",
        "--ntasks-per-node=8",
        "--cpus-per-task=16",
        "--mem=0",
        "--mpi=pmix",
        "all_reduce_perf_mpi",
        "-b",
        "512M",
        "-e",
        "16G",
        "-f",
        "2",
        "-g",
        "1",
      ],
    )

  def test_builds_gb300_command_with_partition_and_segment(self):
    command = nccl_test.build_nccl_command(
      args(partition="gpu", segment=16), self.gb300
    )

    self.assertIn("--partition=gpu", command)
    self.assertIn("--segment=16", command)
    self.assertIn("--gpus-per-node=4", command)
    self.assertIn("--ntasks-per-node=4", command)
    self.assertIn("--cpus-per-task=28", command)

  def test_builds_b300_command_with_nodelist_and_immediate(self):
    b300 = next(
      profile for profile in nccl_test.PLATFORM_PROFILES if profile.name == "B300"
    )

    command = nccl_test.build_nccl_command(
      args(nodelist=("worker-0", "worker-1"), immediate=120), b300
    )

    self.assertIn("--nodelist=worker-0,worker-1", command)
    self.assertIn("--immediate=120", command)
    self.assertIn("--gpus-per-node=8", command)
    self.assertIn("--cpus-per-task=24", command)

  def test_platform_resource_profiles(self):
    expected_profiles = {
      "H100": (8, 16, range(8)),
      "H200": (8, 16, range(8)),
      "B200": (8, 20, range(4, 12)),
      "B300": (8, 24, range(4, 12)),
      "GB300": (4, 28, range(4)),
    }

    for profile in nccl_test.PLATFORM_PROFILES:
      with self.subTest(platform=profile.name):
        gpus, cpus, device_indexes = expected_profiles[profile.name]
        self.assertEqual(profile.gpus_per_node, gpus)
        self.assertEqual(profile.cpus_per_task, cpus)
        self.assertEqual(
          profile.ucx_net_devices,
          ",".join(f"mlx5_{index}:1" for index in device_indexes),
        )

  def test_passes_tunable_arguments_to_srun_and_nccl(self):
    command = nccl_test.build_nccl_command(
      args(
        partition="main",
        min_message_size="1M",
        max_message_size="2G",
        message_step_factor="4",
        time_limit="1:00:00",
      ),
      self.h200,
    )

    self.assertIn("--partition=main", command)
    self.assertIn("--time=1:00:00", command)
    self.assertEqual(
      command[-9:],
      ["all_reduce_perf_mpi", "-b", "1M", "-e", "2G", "-f", "4", "-g", "1"],
    )

  def test_rejects_segment_for_non_gb300(self):
    with self.assertRaisesRegex(ValueError, "only on GB300"):
      nccl_test.validate_platform_args(args(segment=16), self.h200)

  def test_builds_common_and_gb300_environment(self):
    with mock.patch.dict(nccl_test.os.environ, {"PRESERVED": "value"}, clear=True):
      environment = nccl_test.build_environment(self.gb300)

    self.assertEqual(environment["PRESERVED"], "value")
    self.assertEqual(environment["UCX_NET_DEVICES"], "mlx5_0:1,mlx5_1:1,mlx5_2:1,mlx5_3:1")
    self.assertEqual(environment["NCCL_IB_HCA"], "mlx5_0:1,mlx5_1:1,mlx5_2:1,mlx5_3:1")
    self.assertEqual(environment["NCCL_IB_QPS_PER_CONNECTION"], "2")
    self.assertEqual(environment["NCCL_NVLS_ENABLE"], "1")
    self.assertEqual(environment["NCCL_BUFFSIZE"], "8388608")
    self.assertEqual(environment["OMPI_MCA_coll_hcoll_enable"], "0")
    self.assertEqual(environment["OMPI_MCA_coll_ucc_enable"], "0")


class MainTests(unittest.TestCase):
  def setUp(self):
    self.h200 = next(
      profile for profile in nccl_test.PLATFORM_PROFILES if profile.name == "H200"
    )

  @mock.patch.object(nccl_test, "build_environment", return_value={"TEST": "1"})
  @mock.patch.object(nccl_test, "detect_platform")
  @mock.patch.object(nccl_test.subprocess, "run")
  def test_streams_job_output_and_returns_srun_exit_code(
    self, run, detect_platform, build_environment
  ):
    detect_platform.return_value = self.h200
    run.return_value = subprocess.CompletedProcess([], 9)

    exit_code = nccl_test.main(["--nodes", "2", "--partition", "main"])

    self.assertEqual(exit_code, 9)
    command = run.call_args.args[0]
    self.assertIn("--partition=main", command)
    self.assertNotIn("stdout", run.call_args.kwargs)
    self.assertNotIn("stderr", run.call_args.kwargs)
    self.assertNotIn("capture_output", run.call_args.kwargs)
    self.assertEqual(run.call_args.kwargs["env"], {"TEST": "1"})
    build_environment.assert_called_once_with(self.h200)

  @mock.patch.object(nccl_test, "detect_platform")
  @mock.patch.object(nccl_test.subprocess, "run")
  def test_rejects_invalid_nodelist_before_platform_probe(self, run, detect_platform):
    exit_code = nccl_test.main(
      ["--nodes", "2", "--nodelist", "worker-0", "--immediate", "120"]
    )

    self.assertEqual(exit_code, 2)
    detect_platform.assert_not_called()
    run.assert_not_called()

  @mock.patch.object(nccl_test, "detect_platform")
  @mock.patch.object(nccl_test.subprocess, "run")
  def test_rejects_segment_before_main_job(self, run, detect_platform):
    detect_platform.return_value = self.h200

    exit_code = nccl_test.main(["--nodes", "2", "--segment", "16"])

    self.assertEqual(exit_code, 2)
    run.assert_not_called()


if __name__ == "__main__":
  unittest.main()
