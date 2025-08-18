import datetime
import functools
import json
import logging
import os
import string
import subprocess
import sys
import time
import typing

# Set up logging
try:
  log_stdout = "/dev/stdout"
  log_path = os.environ.get("CHECKS_RUNNER_OUTPUT", log_stdout)
  if log_path != log_stdout:
    log_dir = os.path.dirname(log_path)
    os.makedirs(log_dir, mode=0o777, exist_ok=True)
    if not os.path.exists(log_path):
      open(log_path, 'w').close()
  logging.Formatter.converter = time.gmtime
  logging.basicConfig(
    filename=log_path,
    filemode='w',
    format='[%(asctime)s.%(msecs)03d UTC] %(levelname)s: %(message)s',
    datefmt='%Y-%m-%d %H:%M:%S',
    level=logging.INFO
  )
except Exception as e:
  print(f"Failed to set up logging, exiting: {e}")
  sys.exit(0)

class Check(typing.NamedTuple):
  # Name of the check.
  # It's used for logging and string substitutions. Doesn't have to be unique.
  name: str = "noname"

  # Command to run (will be executed in bash).
  command: str = "/usr/bin/true"

  # Nodes with what platforms this check should run on.
  # Supported values:
  # - "any" - run on any platform and skip platform detection
  # - "CPU" - run on nodes without GPUs
  # - "<num>xGPU" - run on nodes with <num> GPUs of any model
  # - "<num>x<gpu_model>" - run on nodes with <num> GPUs of model <gpu_model>
  platforms: typing.List[str] = ["any"]

  # Whether to skip this check for jobs that don't allocate GPUs.
  # Allows to skip the check for CPU-only jobs in "prolog" and "epilog" contexts even if the node is equipped with GPUs.
  skip_for_cpu_jobs: bool = False

  # What contexts this check should run in.
  # Supported values:
  # - "any" - any context
  # - "none" - never run
  # - "prolog" - run in Slurm job Prolog script (on each node, before the job)
  # - "epilog" - run in Slurm job Epilog script (on each node, after the job)
  # - "hc_program" - run in Slurm HealthCheckProgram script (on each node, periodically)
  contexts: typing.List[str] = ["any"]

  # Nodes in what states this check should run on.
  # Supported values:
  # - "any" - run on nodes in any state and skip state detection
  # - "drain" - run on drained/draining nodes
  node_states: typing.List[str] = ["any"]

  # Action to do when the command fails.
  # Supported values:
  # - "none" - do nothing
  # - "drain" - drain the node
  # - "comment" - comment the node
  on_fail: str = "none"

  # Action to do when the command completes successfully.
  # Supported values:
  # - "none" - do nothing
  # - "undrain" - undrain the node if it's drained with the same reason (details can differ)
  # - "uncomment" - uncomment the node if it was commented with the same reason (details can differ)
  # Please note that "undrain" and "uncomment" actions can be issues only from "hc_program" context.
  on_ok: str = "none"

  # Template of the reason message prefix used for (un)draining or (un)commenting nodes.
  # Supported substitutions:
  # - $name - name of the check
  # - $context - context in which the check is running in (one of: "prolog", "epilog", "hc_program")
  # The full reason looks like "$reason [$context]" or "$reason: $details [$context]" (when reason_append_details is True).
  reason_base: str = "[node_problem] $name"

  # Whether to append details to the reason message.
  # Details is the message that the check command printed to its file descriptor 3.
  reason_append_details: bool = True

  # Whether to run this check inside chroot into the jail rootfs.
  run_in_jail: bool = False

  # Path template to save stderr and stdout of the command.
  # Relative to $CHECKS_OUTPUTS_BASE_DIR.
  # Supported substitutions:
  # - $worker - name of the Slurm node
  # - $name - name of the check
  # - $context - context in which the check is running in (one of: "prolog", "epilog", "hc_program")
  log: str = "slurm_scripts/$worker.$name.$context.out"

  # Whether to export additional environment variables
  # Available variables:
  # - CHECKS_PLATFORM_TAG - the most precise platform tag
  # - CHECKS_PLATFORM_TAGS - comma-separated list of all platform tags
  # - CHECKS_NODE_STATE_FLAGS - "+"-separated list of Slurm node state flags
  # - CHECKS_NODE_REASON - (drain/down) reason field of the Slurm node
  # - CHECKS_NODE_COMMENT - comment field of the Slurm node
  # - CHECKS_NODE_REAL_MEM_BYTES - total allocatable memory in bytes for the Slurm node
  # - CHECKS_JOB_ALLOC_MEM_BYTES - memory in bytes, allocated for this Slurm job on this node
  # These values are extracted from long-running commands, that's why they aren't exported by default
  need_env: list[str] = []

class NodeInfo(typing.NamedTuple):
  state_flags: typing.List[str] = []
  reason: str = ""
  comment: str = ""
  real_memory_bytes: int = 0

class JobInfo(typing.NamedTuple):
  allocated_memory_bytes: int = 0

# Get environment variables
try:
  SLURMD_NODENAME = os.environ["SLURMD_NODENAME"]
  SLURM_JOB_ID = os.environ.get("SLURM_JOB_ID", "") # Not available in the "hc_program" context
  SLURM_JOB_GPUS = os.environ.get("SLURM_JOB_GPUS", "") # Not available in the "hc_program context"
  SLURM_JOB_COMMENT = os.environ.get("SLURM_JOB_COMMENT", "") # Not available in the "hc_program context"
  CHECKS_OUTPUTS_BASE_DIR = os.environ["CHECKS_OUTPUTS_BASE_DIR"]
  CHECKS_CONTEXT = os.environ["CHECKS_CONTEXT"]
  CHECKS_CONFIG = os.environ["CHECKS_CONFIG"]
except KeyError as ke:
  logging.error(f"Failed to get environment variable '{ke.args[0]}', exiting: {ke}")
  sys.exit(0)

def main():
  start_time = datetime.datetime.now()
  logging.info("Started")

  # Print environment
  for key, value in os.environ.items():
    slurm_var = key.startswith("SLURM_") or key.startswith("SLURMD_") or key.startswith("CUDA_")
    check_runner_var = key.startswith("CHECKS_")
    path_var = key == "PATH"
    if slurm_var or check_runner_var or path_var:
      logging.info(f"Environment {key}=\"{value}\"")

  # Skip all checks if requested in the job comment
  if SLURM_JOB_COMMENT == "skip_checks":
    logging.info("Job has comment 'skip_checks', exiting")
    exit(0)

  # Load checks from a config file
  try:
    with open(CHECKS_CONFIG) as f:
      checks_data = json.load(f)
    checks = [Check(**entry) for entry in checks_data]
  except Exception as e:
    logging.error(f"Failed to open checks config {CHECKS_CONFIG}, exiting: {e}")
    sys.exit(0)

  # Filter checks
  applicable_checks = filter_applicable_checks(checks)

  # Run checks on the host (container) filesystem
  host_checks = [c for c in applicable_checks if not c.run_in_jail]
  if host_checks:
    chdir_into_checks_dir()
    for check in host_checks:
      run_check(check, in_jail=False)

  # Run checks on the jail filesystem
  jail_checks = [c for c in applicable_checks if c.run_in_jail]
  if jail_checks:
    chroot_into_jail()
    chdir_into_checks_dir()
    for check in jail_checks:
      run_check(check, in_jail=True)

  end_time = datetime.datetime.now()
  logging.info(f"Finished in {(end_time - start_time).total_seconds()} seconds")
  sys.exit(0)

# Filter checks for the current environment
def filter_applicable_checks(checks: typing.List[Check]) -> typing.List[Check]:
  # Filter by context and skip_for_cpu_jobs
  slurm_job_cpu_only: bool = SLURM_JOB_GPUS == "" and CHECKS_CONTEXT in ["prolog", "epilog"]
  applicable_checks = [
    check for check in checks
    if ("any" in check.contexts or CHECKS_CONTEXT in check.contexts)
       and not (check.skip_for_cpu_jobs and slurm_job_cpu_only)
  ]

  # Only detect platform tags and filter by it if needed
  needs_platform_filter = any(
    "any" not in check.platforms for check in applicable_checks
  )
  if needs_platform_filter:
    platform_tags = get_platform_tags()

    # Filter by platform
    applicable_checks = [
      check for check in applicable_checks
      if (
        "any" in check.platforms or
        any(tag in check.platforms for tag in platform_tags)
      )
    ]

  # Only detect node state and filter by it if needed
  needs_node_state_filter = any(
    "any" not in check.node_states for check in applicable_checks
  )
  if needs_node_state_filter:
    node_info = get_node_info()

    # Filter by node state
    applicable_checks = [
      check for check in applicable_checks
      if (
        "any" in check.node_states or
        ("drain" in check.node_states and "DRAIN" in node_info.state_flags)
      )
    ]

  return applicable_checks

# Run a specific check
def run_check(check: Check, in_jail=False):
  # Export environment variables requested by this check
  export_needed_env(check)

  # Print info about the running check
  log_rel_path = string.Template(check.log).safe_substitute(
    worker=SLURMD_NODENAME, context=CHECKS_CONTEXT, name=check.name
  )
  log_abs_path = os.path.join(CHECKS_OUTPUTS_BASE_DIR, log_rel_path)
  if not in_jail:
    log_abs_path = "/mnt/jail" + log_abs_path
  start_time = datetime.datetime.now()
  logging.info(f"Running check {check.name} ({check.command}), logging to {log_abs_path}")
  logging.info(f"Check spec: {json.dumps(check._asdict(), indent=2)}")

  # Create parent dirs with full permissions
  os.makedirs(os.path.dirname(log_abs_path), mode=0o777, exist_ok=True)

  # Execute the check command
  cmd = ["bash", "-c", f"{check.command} 3>&1 1>\"{log_abs_path}\" 2>&1"]
  result = subprocess.run(cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE, universal_newlines=True)

  # Build the reason message
  reason_base = string.Template(check.reason_base.rstrip()).safe_substitute(
    context=CHECKS_CONTEXT, name=check.name
  )
  reason = reason_base
  details = result.stdout.strip().encode('unicode_escape').decode()
  if check.reason_append_details and details:
    reason += f": {details}"
  reason += f" [{CHECKS_CONTEXT}]"

  # Log check running time
  end_time = datetime.datetime.now()
  logging.info(f"Completed check {check.name} in {(end_time - start_time).total_seconds()} seconds")

  # React to the check result
  if result.returncode != 0:
    logging.info(f"Check {check.name}: FAIL ({details})")

    # Drain / comment the Slurm node
    if check.on_fail == "drain" and "DRAIN" not in get_node_info().state_flags:
      drain_node(reason)
    elif check.on_fail == "comment":
      comment_node(reason)

    # Exit, if the check failed
    # In prolog context, restart the job and print a message to the job output
    # (Slurm passes lines from Prolog stdout that start with "print " to job output)
    if CHECKS_CONTEXT == "prolog":
      print(f"print Slurm healthcheck failed on node {SLURMD_NODENAME}, trying to automatically requeue")
      sys.exit(1)

    sys.exit(0)

  logging.info(f"Check {check.name}: OK")

  # Undrain / uncomment the Slurm node, if it was marked with the same reason
  if check.on_ok == "undrain" and "DRAIN" in get_node_info().state_flags:
    if get_node_info().reason and get_node_info().reason.startswith(reason_base):
      undrain_node()
  elif check.on_ok == "uncomment":
    if get_node_info().comment and get_node_info().comment.startswith(reason_base):
      uncomment_node()

# Export additional environment variables requested by the check
# Their values are obtained from long-running commands, that's why they aren't exported by default
def export_needed_env(check: Check):
  for env in check.need_env:
    if env == "CHECKS_PLATFORM_TAG":
      os.environ["CHECKS_PLATFORM_TAG"] = get_platform_tags()[0]
    if env == "CHECKS_PLATFORM_TAGS":
      os.environ["CHECKS_PLATFORM_TAGS"] = ",".join(get_platform_tags())
    if env == "CHECKS_NODE_STATE_FLAGS":
      os.environ["CHECKS_NODE_STATE_FLAGS"] = "+".join(get_node_info().state_flags)
    if env == "CHECKS_NODE_REASON":
      os.environ["CHECKS_NODE_REASON"] = get_node_info().reason
    if env == "CHECKS_NODE_COMMENT":
      os.environ["CHECKS_NODE_COMMENT"] = get_node_info().comment
    if env == "CHECKS_NODE_REAL_MEM_BYTES":
      os.environ["CHECKS_NODE_REAL_MEM_BYTES"] = str(get_node_info().real_memory_bytes)
    if env == "CHECKS_JOB_ALLOC_MEM_BYTES":
      os.environ["CHECKS_JOB_ALLOC_MEM_BYTES"] = str(get_job_info().allocated_memory_bytes)

# Get GPU platform tags, e.g. ["8xH200", "8xGPU] from "nvidia-smi"
# Please note, this command can be executed from both jail or host rootfs
# The list starts with more specific tags, and ends with less specific ones
# It's guaranteed that the list has at least one item
# This function returns the cached value for subsequent calls
@functools.lru_cache(maxsize=1)
def get_platform_tags() -> typing.List[str]:
  try:
    # Warning: this command can be executed from both jail or host rootfs
    res = subprocess.run(
      ["nvidia-smi", "--query-gpu=name", "--format=csv,noheader"],
      check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, universal_newlines=True
    )
    gpu_names = [line.strip() for line in res.stdout.strip().splitlines() if line.strip()]
    count = len(gpu_names)

    if count == 0:
      return ["CPU"]

    first = gpu_names[0]
    all_same = all(name == first for name in gpu_names)

    tags = []
    if all_same:
      if "H100" in first:
        tags.append(f"{count}xH100")
      elif "H200" in first:
        tags.append(f"{count}xH200")
      elif "B200" in first:
        tags.append(f"{count}xB200")

    tags.append(f"{count}xGPU")

    logging.info(f"Detected platform tags: {', '.join(tags)}")
    return tags

  except Exception as e:
    logging.warning(f"Failed to detect GPU platform, assuming 'CPU': {e}")
    return ["CPU"]

# Get info about the Slurm node from "scontrol show node"
# Please note, this command can be executed from both jail or host rootfs
# This function returns the cached value for subsequent calls
@functools.lru_cache(maxsize=1)
def get_node_info() -> NodeInfo:
  try:
    result = subprocess.run(
      ["scontrol", "show", "node", SLURMD_NODENAME, "--json"],
      check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, universal_newlines=True
    )
    json_out = result.stdout.strip()
    data = json.loads(json_out)

    nodes = data.get("nodes", [])
    if not nodes:
      raise ValueError("No nodes data found")

    node = nodes[0]
    real_memory_mib = node.get("real_memory", 0)

    info = NodeInfo(
      state_flags=node.get("state", []),
      reason=node.get("reason", ""),
      comment=node.get("comment", ""),
      real_memory_bytes=(real_memory_mib * 1024 * 1024)
    )
    logging.info(f"Slurm node info: {json.dumps(info._asdict(), indent=2)}")
    return info
  except Exception as e:
    logging.warning(f"Failed to get info about Slurm node {SLURMD_NODENAME}: {e}")
    return NodeInfo()

# Get info about the Slurm job from "scontrol show job"
# Please note, this command can be executed from both jail or host rootfs
# This function returns the cached value for subsequent calls
@functools.lru_cache(maxsize=1)
def get_job_info() -> JobInfo:
  try:
    if CHECKS_CONTEXT not in ("prolog", "epilog"):
      logging.warning(f"Requested Slurm job info from an unsupported context: {CHECKS_CONTEXT}")
      return JobInfo()

    # Warning: this command can be executed from both jail or host rootfs
    result = subprocess.run(
      ["scontrol", "show", "job", SLURM_JOB_ID, "--json"],
      check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, universal_newlines=True
    )
    json_out = result.stdout.strip()
    data = json.loads(json_out)

    jobs = data.get("jobs", [])
    if not jobs:
      raise ValueError("No jobs data found")

    job = jobs[0]
    allocated_memory_mib = 0
    job_resources = job.get("job_resources", {}).get("nodes", {}).get("allocation", [])
    for allocation in job_resources:
      if allocation.get("name") == SLURMD_NODENAME:
        allocated_memory_mib = allocation.get("memory", {}).get("allocated", 0)
        break

    info = JobInfo(
      allocated_memory_bytes=(allocated_memory_mib * 1024 * 1024),
    )
    logging.info(f"Slurm job info: {json.dumps(info._asdict(), indent=2)}")
    return info
  except Exception as e:
    logging.warning(f"Failed to get info about Slurm job {SLURM_JOB_ID}: {e}")
    return JobInfo()

# Open the directory where checks are located
def chdir_into_checks_dir():
  try:
    os.chdir("/opt/slurm_scripts")
  except Exception as e:
    logging.error(f"Failed to chdir into /opt/slurm_scripts, exiting: {e}")
    sys.exit(0)

# Chroot into jail rootfs directory
def chroot_into_jail():
  try:
    os.chroot("/mnt/jail")
  except Exception as e:
    logging.error(f"Failed to chroot into /mnt/jail, exiting: {e}")
    sys.exit(0)

# Drain the Slurm node with a specific reason
def drain_node(reason):
  logging.info(f"Drain Slurm node {SLURMD_NODENAME}: {reason}")
  try:

    subprocess.run(
      ["scontrol", "update", f"NodeName={SLURMD_NODENAME}", "State=drain", f"Reason=\"{reason}\""],
      check=False, stderr=subprocess.DEVNULL
    )
    # Invalidate cache for the Slurm node info
    get_node_info.cache_clear()
  except Exception as e:
    logging.warning(f"Failed to drain Slurm node {SLURMD_NODENAME}: {e}")

# Undrain the Slurm node
def undrain_node():
  logging.info(f"Undrain Slurm node {SLURMD_NODENAME}")
  try:
    subprocess.run(
      ["scontrol", "update", f"NodeName={SLURMD_NODENAME}", "State=resume"],
      check=False, stderr=subprocess.DEVNULL
    )
    # Invalidate cache for the Slurm node info
    get_node_info.cache_clear()
  except Exception as e:
    logging.warning(f"Failed to undrain Slurm node {SLURMD_NODENAME}: {e}")

# Comment the Slurm node
def comment_node(comment):
  logging.info(f"Comment Slurm node {SLURMD_NODENAME}: {comment}")
  try:
    subprocess.run(
      ["scontrol", "update", f"NodeName={SLURMD_NODENAME}", f"Comment=\"{comment}\""],
      check=False, stderr=subprocess.DEVNULL
    )
    # Invalidate cache for the Slurm node info
    get_node_info.cache_clear()
  except Exception as e:
    logging.warning(f"Failed to comment Slurm node {SLURMD_NODENAME}: {e}")

# Uncomment the Slurm node
def uncomment_node():
  logging.info(f"Uncomment Slurm node {SLURMD_NODENAME}")
  try:
    subprocess.run(
      ["scontrol", "update", f"NodeName={SLURMD_NODENAME}", "Comment=\"\""],
      check=False, stderr=subprocess.DEVNULL
    )
    # Invalidate cache for the Slurm node info
    get_node_info.cache_clear()
  except Exception as e:
    logging.warning(f"Failed to uncomment Slurm node {SLURMD_NODENAME}: {e}")

try:
  if __name__ == "__main__":
    main()
except Exception as e:
  logging.error(f"Unknown error: {e}")
  exit(0)
