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
      ["--nodes", "2", "--nodelist", ""],
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
      ["--nodes", "2", "--nodelist", "worker-[0-1]", "--immediate", "120"]
    )

    self.assertEqual(parsed.nodelist, "worker-[0-1]")
    self.assertEqual(parsed.immediate, 120)


class SlurmMetadataTests(unittest.TestCase):
  @mock.patch.object(nccl_test, "run_scontrol_json")
  def test_resolves_default_partition(self, run_scontrol_json):
    run_scontrol_json.return_value = {
      "partitions": [
        {"name": "gpu-b300", "flags": [], "nodes": {"configured": "b-[0-1]"}},
        {
          "name": "gpu-h200",
          "flags": ["DEFAULT"],
          "nodes": {"configured": "h-[0-3]"},
        },
      ]
    }

    partition, hostlist = nccl_test.resolve_partition(None)

    self.assertEqual(partition, "gpu-h200")
    self.assertEqual(hostlist, "h-[0-3]")
    run_scontrol_json.assert_called_once_with(["show", "partitions"])

  @mock.patch.object(nccl_test, "run_scontrol_json")
  def test_resolves_explicit_partition(self, run_scontrol_json):
    run_scontrol_json.return_value = {
      "partitions": [
        {"name": "gpu-b300", "flags": [], "nodes": {"configured": "b-[0-1]"}}
      ]
    }

    partition, hostlist = nccl_test.resolve_partition("gpu-b300")

    self.assertEqual(partition, "gpu-b300")
    self.assertEqual(hostlist, "b-[0-1]")
    run_scontrol_json.assert_called_once_with(
      ["show", "partition", "gpu-b300"]
    )

  @mock.patch.object(nccl_test, "run_scontrol_json")
  def test_rejects_missing_default_partition(self, run_scontrol_json):
    run_scontrol_json.return_value = {
      "partitions": [
        {"name": "gpu", "flags": [], "nodes": {"configured": "worker-0"}}
      ]
    }

    with self.assertRaisesRegex(ValueError, "expected one default"):
      nccl_test.resolve_partition(None)

  @mock.patch.object(nccl_test, "run_scontrol", return_value="worker-0\nworker-1\n")
  def test_expands_slurm_hostlist(self, run_scontrol):
    nodes = nccl_test.expand_hostlist("worker-[0-1]")

    self.assertEqual(nodes, ("worker-0", "worker-1"))
    run_scontrol.assert_called_once_with(
      ["show", "hostnames", "worker-[0-1]"]
    )

  def test_parses_typed_gpu_gres(self):
    node = {
      "name": "worker-0",
      "gres": "gpu:nvidia_h200:4(S:0),gpu:nvidia_h200:4(S:1)",
      "cpus": 128,
    }

    gpu_node = nccl_test.parse_gpu_node(node)

    self.assertEqual(
      gpu_node,
      nccl_test.GPUNode("worker-0", "nvidia_h200", 8, 128),
    )

  def test_ignores_cpu_node(self):
    self.assertIsNone(
      nccl_test.parse_gpu_node({"name": "cpu-0", "gres": "", "cpus": 32})
    )

  def test_rejects_untyped_gpu_gres(self):
    with self.assertRaisesRegex(ValueError, "untyped or invalid"):
      nccl_test.parse_gpu_node(
        {"name": "worker-0", "gres": "gpu:8", "cpus": 128}
      )


class PlatformDetectionTests(unittest.TestCase):
  @staticmethod
  def node(
    name,
    gres="gpu:nvidia_h200:8(S:0-1)",
    cpus=128,
    partitions=("main",),
  ):
    return {
      "name": name,
      "gres": gres,
      "cpus": cpus,
      "partitions": list(partitions),
    }

  def detect(self, nodes, arguments=None, expanded_nodes=None):
    arguments = arguments or args()
    expanded_nodes = expanded_nodes or tuple(node["name"] for node in nodes)
    with (
      mock.patch.object(
        nccl_test, "resolve_partition", return_value=("main", "worker-[0-1]")
      ),
      mock.patch.object(
        nccl_test, "expand_hostlist", return_value=expanded_nodes
      ),
      mock.patch.object(nccl_test, "load_nodes", return_value=nodes),
    ):
      return nccl_test.detect_platform(arguments)

  def test_detects_supported_platforms_and_derives_resources(self):
    platforms = {
      "nvidia_h100_80gb_hbm3": ("H100", 8, 16, 128),
      "nvidia_h200": ("H200", 8, 16, 128),
      "nvidia_b200": ("B200", 8, 20, 160),
      "nvidia_b300": ("B300", 8, 24, 192),
      "nvidia_gb300": ("GB300", 4, 28, 112),
    }

    for gres_type, (name, gpu_count, cpus_per_task, cpu_count) in platforms.items():
      with self.subTest(platform=name):
        nodes = [
          self.node(f"worker-{index}", f"gpu:{gres_type}:{gpu_count}", cpu_count)
          for index in range(2)
        ]

        platform = self.detect(nodes)

        self.assertEqual(platform.name, name)
        self.assertEqual(platform.gres_type, gres_type)
        self.assertEqual(platform.gpus_per_node, gpu_count)
        self.assertEqual(platform.cpus_per_task, cpus_per_task)

  def test_filters_cpu_nodes_from_partition(self):
    nodes = [
      self.node("cpu-0", "", 32),
      self.node("worker-0"),
      self.node("worker-1"),
    ]

    platform = self.detect(nodes)

    self.assertEqual(platform.name, "H200")

  def test_rejects_cpu_node_in_explicit_nodelist(self):
    nodes = [self.node("cpu-0", "", 32), self.node("worker-0")]

    with self.assertRaisesRegex(ValueError, "cpu-0 has no GPU GRES"):
      self.detect(nodes, args(nodelist="cpu-0,worker-0"))

  def test_rejects_nodelist_with_wrong_expanded_node_count(self):
    nodes = [self.node("worker-0")]

    with self.assertRaisesRegex(ValueError, "contains 1 node"):
      self.detect(
        nodes,
        args(nodelist="worker-0"),
        expanded_nodes=("worker-0",),
      )

  def test_rejects_node_outside_target_partition(self):
    nodes = [
      self.node("worker-0"),
      self.node("worker-1", partitions=("other",)),
    ]

    with self.assertRaisesRegex(ValueError, "not in partition 'main'"):
      self.detect(nodes)

  def test_rejects_mixed_gpu_types(self):
    nodes = [
      self.node("worker-0"),
      self.node("worker-1", "gpu:nvidia_b300:8", 192),
    ]

    with self.assertRaisesRegex(ValueError, "different GPU GRES types"):
      self.detect(nodes)

  def test_rejects_inconsistent_node_resources(self):
    nodes = [self.node("worker-0"), self.node("worker-1", cpus=120)]

    with self.assertRaisesRegex(ValueError, "different resource configurations"):
      self.detect(nodes)

  def test_rejects_insufficient_gpu_nodes(self):
    nodes = [self.node("cpu-0", "", 32), self.node("worker-0")]

    with self.assertRaisesRegex(ValueError, "contains 1 GPU node"):
      self.detect(nodes)

  def test_rejects_unsupported_gpu_type(self):
    nodes = [
      self.node("worker-0", "gpu:nvidia_future:8"),
      self.node("worker-1", "gpu:nvidia_future:8"),
    ]

    with self.assertRaisesRegex(ValueError, "unsupported GPU GRES type"):
      self.detect(nodes)

  def test_rejects_cpu_count_not_divisible_by_gpu_count(self):
    nodes = [
      self.node("worker-0", "gpu:nvidia_h200:8", 127),
      self.node("worker-1", "gpu:nvidia_h200:8", 127),
    ]

    with self.assertRaisesRegex(ValueError, "cannot distribute 127 CPUs"):
      self.detect(nodes)


class CommandTests(unittest.TestCase):
  def setUp(self):
    self.h200 = nccl_test.DetectedPlatform(
      name="H200",
      gres_type="nvidia_h200",
      gpus_per_node=8,
      cpus_per_task=16,
    )
    self.gb300 = nccl_test.DetectedPlatform(
      name="GB300",
      gres_type="nvidia_gb300",
      gpus_per_node=4,
      cpus_per_task=28,
      environment={
        "OMPI_MCA_coll_hcoll_enable": "0",
        "OMPI_MCA_coll_ucc_enable": "0",
      },
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
        "--gpus-per-node=nvidia_h200:8",
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
    self.assertIn("--gpus-per-node=nvidia_gb300:4", command)
    self.assertIn("--ntasks-per-node=4", command)
    self.assertIn("--cpus-per-task=28", command)

  def test_builds_b300_command_with_nodelist_and_immediate(self):
    b300 = nccl_test.DetectedPlatform(
      name="B300",
      gres_type="nvidia_b300",
      gpus_per_node=8,
      cpus_per_task=24,
    )

    command = nccl_test.build_nccl_command(
      args(nodelist="worker-[0-1]", immediate=120), b300
    )

    self.assertIn("--nodelist=worker-[0-1]", command)
    self.assertIn("--immediate=120", command)
    self.assertIn("--gpus-per-node=nvidia_b300:8", command)
    self.assertIn("--cpus-per-task=24", command)

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

  def test_builds_common_and_gb300_environment_without_network_overrides(self):
    with mock.patch.dict(nccl_test.os.environ, {"PRESERVED": "value"}, clear=True):
      environment = nccl_test.build_environment(self.gb300)

    self.assertEqual(environment["PRESERVED"], "value")
    self.assertNotIn("UCX_NET_DEVICES", environment)
    self.assertNotIn("NCCL_IB_HCA", environment)
    self.assertEqual(environment["NCCL_IB_QPS_PER_CONNECTION"], "2")
    self.assertEqual(environment["NCCL_NVLS_ENABLE"], "1")
    self.assertEqual(environment["NCCL_BUFFSIZE"], "8388608")
    self.assertEqual(environment["OMPI_MCA_coll_hcoll_enable"], "0")
    self.assertEqual(environment["OMPI_MCA_coll_ucc_enable"], "0")


class MainTests(unittest.TestCase):
  def setUp(self):
    self.h200 = nccl_test.DetectedPlatform(
      name="H200",
      gres_type="nvidia_h200",
      gpus_per_node=8,
      cpus_per_task=16,
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
  def test_reports_platform_detection_error(self, run, detect_platform):
    detect_platform.side_effect = ValueError(
      "--nodelist contains 1 node(s), expected 2"
    )

    exit_code = nccl_test.main(["--nodes", "2", "--nodelist", "worker-0"])

    self.assertEqual(exit_code, 2)
    run.assert_not_called()

  @mock.patch.object(nccl_test, "detect_platform")
  @mock.patch.object(nccl_test.subprocess, "run")
  def test_reports_scontrol_error(self, run, detect_platform):
    detect_platform.side_effect = nccl_test.ScontrolError("scontrol failed")

    exit_code = nccl_test.main(["--nodes", "2"])

    self.assertEqual(exit_code, 1)
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
