import os
import subprocess
import sys
import json
import string
import datetime
import typing

SLURMD_NODENAME = ""
SLURM_JOB_GPUS = ""
CHECKS_OUTPUTS_BASE_DIR = ""
CHECKS_CONTEXT = ""
CHECKS_CONFIG = ""

class Check(typing.NamedTuple):
  name: str = "noname"
  command: str = "true"
  on_fail: str = "none"
  on_ok: str = "none"
  reason: str = "[node_problem] $name"
  append_details: bool = False
  supported_platforms: list[str] = ["any"]
  run_in_jail: bool = False
  skip_for_cpu_jobs: bool = True
  log: str = "slurm_scripts/$worker.$name.$context.out"
  contexts: list[str] = ["any"]

class NodeInfo(typing.NamedTuple):
  state_flags: list[str] = []
  reason: str = ""
  comment: str = ""

def main():
  # Environment
  global SLURMD_NODENAME, SLURM_JOB_GPUS, CHECKS_OUTPUTS_BASE_DIR, CHECKS_CONTEXT, CHECKS_CONFIG
  try:
    SLURMD_NODENAME = os.environ["SLURMD_NODENAME"]
    SLURM_JOB_GPUS = os.environ.get("SLURM_JOB_GPUS", "")
    CHECKS_OUTPUTS_BASE_DIR = os.environ["CHECKS_OUTPUTS_BASE_DIR"]
    CHECKS_CONTEXT = os.environ["CHECKS_CONTEXT"]
    CHECKS_CONFIG = os.environ["CHECKS_CONFIG"]
  except Exception as e:
    print(f"Failed to get environment variable '{e.args[0]}', exiting: {e}", file=sys.stderr)
    sys.exit(0)

  # Load checks from a config file
  try:
    with open(CHECKS_CONFIG) as f:
      checks_data = json.load(f)
    checks = [Check(**entry) for entry in checks_data]
  except Exception as e:
    print(f"Failed to open checks config {CHECKS_CONFIG}, exiting: {e}", file=sys.stderr)
    sys.exit(0)

  # Filter checks
  applicable_checks = filter_applicable_checks(checks)

  # Run checks on the host (container) filesystem
  host_checks = [c for c in applicable_checks if not c.run_in_jail]
  if host_checks:
    print("Running checks on the host filesystem:")
    print(json.dumps(host_checks, indent=2))
    chdir_into_checks_dir()
    for check in host_checks:
      run_check(check, in_jail=False)

  # Run checks on the jail filesystem
  jail_checks = [c for c in applicable_checks if c.run_in_jail]
  if jail_checks:
    print("Running checks on the jail filesystem:")
    print(json.dumps(jail_checks, indent=2))
    chroot_into_jail()
    chdir_into_checks_dir()
    for check in jail_checks:
      run_check(check, in_jail=True)

  sys.exit(0)

# Filter checks for the current environment
def filter_applicable_checks(checks: list[Check]) -> list[Check]:
  slurm_job_cpu_only = SLURM_JOB_GPUS == "" and CHECKS_CONTEXT in ["prolog", "epilog"]

  # Filter by context and skip_for_cpu_jobs
  applicable_checks = [
    check for check in checks
    if ("any" in check.contexts or CHECKS_CONTEXT in check.contexts)
       and not (check.skip_for_cpu_jobs and slurm_job_cpu_only)
  ]

  # Check if any applicable check needs GPU platform filtering
  needs_platform_filter = any(
    "any" not in check.supported_platforms for check in applicable_checks
  )

  # Only compute platform tags if needed
  if needs_platform_filter:
    platform_tags = get_gpu_platform_tags()
    os.environ["CHECKS_PLATFORM_TAG"] = platform_tags[0]

    # Filter by platform
    applicable_checks = [
      check for check in applicable_checks
      if (
        "any" in check.supported_platforms or
        any(tag in check.supported_platforms for tag in platform_tags)
      )
    ]

  return applicable_checks

# Get GPU platform tags, e.g. ["8xH200", "8xGPU]
# The list starts with more specific tags, and ends with less specific ones
# It's guaranteed that the list has at least one item
def get_gpu_platform_tags() -> list[str]:
  try:
    res = subprocess.run(
      "nvidia-smi --query-gpu=name --format=csv,noheader",
      shell=True, check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, universal_newlines=True
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
    return tags

  except Exception as e:
    print(f"Failed to detect GPU platform, assuming 'CPU': {e}", file=sys.stderr)
    return ["CPU"]

# Run a specific check
def run_check(check: Check, in_jail=False):
  # Print info about the running check
  timestamp = datetime.datetime.now().astimezone().isoformat()
  rootfs = "jail" if in_jail else "host"
  log_rel_path = string.Template(check.log).safe_substitute(
    worker=SLURMD_NODENAME, context=CHECKS_CONTEXT, name=check.name
  )
  log_abs_path = os.path.join(CHECKS_OUTPUTS_BASE_DIR, log_rel_path)
  if not in_jail:
    log_abs_path = "/mnt/jail" + log_abs_path
  print(f"[{timestamp}] <{rootfs}> Running check {check.name} ({check.command}), logging to {log_abs_path}")

  # Create parent dirs with full permissions
  os.makedirs(os.path.dirname(log_abs_path), mode=0o777, exist_ok=True)

  # Execute the check command
  cmd = ["bash", "-c", f"{check.command} 3>&1 1>\"{log_abs_path}\" 2>&1"]
  result = subprocess.run(cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE, universal_newlines=True)

  # Build the reason message
  reason_base = string.Template(check.reason.rstrip()).safe_substitute(
    context=CHECKS_CONTEXT, name=check.name
  )
  reason = reason_base
  details = result.stdout.strip()
  if check.append_details and details:
    reason += f": {details}"
  reason += f" [{CHECKS_CONTEXT}]"

  # Get info about the Slurm node
  node_info = get_node_info()
  # React to the check result
  if result.returncode == 0:
    print(f"Check {check.name}: PASS")

    # Undrain / uncomment the Slurm node, if it was marked with the same reason
    if check.on_ok == "undrain" and "DRAIN" in node_info.state_flags:
      node_reason = node_info.reason
      if node_reason and node_reason.startswith(reason_base):
        undrain_node()
    elif check.on_ok == "uncomment":
      node_comment = node_info.comment
      if node_comment and node_comment.startswith(reason_base):
        uncomment_node()
  else:
    print(f"Check {check.name}: FAIL ({details})")

    # Drain / comment the Slurm node
    if check.on_fail == "drain" and "DRAIN" not in node_info.state_flags:
      drain_node(reason)
    elif check.on_fail == "comment":
      comment_node(reason)

    # Exit, if the check failed
    # In prolog context, restart the job
    if CHECKS_CONTEXT == "prolog":
      print(f"print Slurm healthcheck failed on node {SLURMD_NODENAME}, trying to automatically requeue")
      sys.exit(1)
    else:
      sys.exit(0)

# Open the directory where checks are located
def chdir_into_checks_dir():
  try:
    os.chdir("/opt/slurm_scripts")
  except Exception as e:
    print(f"Failed to chdir into /opt/slurm_scripts, exiting: {e}", file=sys.stderr)
    sys.exit(0)

# Chroot into jail rootfs directory
def chroot_into_jail():
  try:
    os.chroot("/mnt/jail")
  except Exception as e:
    print(f"Failed to chroot into /mnt/jail, exiting: {e}", file=sys.stderr)
    sys.exit(0)

# Get info about the Slurm node from sinfo
_cached_node_info: typing.Optional[NodeInfo] = None
def get_node_info() -> NodeInfo:
  global _cached_node_info
  if _cached_node_info is not None:
    return _cached_node_info

  try:
    result = subprocess.run(
      ["sinfo", "-n", SLURMD_NODENAME, "--json"],
      check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, universal_newlines=True
    )
    json_out = result.stdout.strip()
    data = json.loads(json_out)

    sinfo = data.get("sinfo", [])
    if not sinfo:
      raise ValueError("No sinfo data found")

    node = sinfo[0].get("node", {})

    _cached_node_info = NodeInfo(
      state_flags=node.get("state", []),
      reason=node.get("reason", {}).get("description", ""),
      comment=node.get("comment", "")
    )
    return _cached_node_info
  except Exception as e:
    print(f"Failed to get info about Slurm node {SLURMD_NODENAME}: {e}", file=sys.stderr)
    return NodeInfo()

# Drain the Slurm node with specific reason
def drain_node(reason):
  print(f"Drain Slurm node {SLURMD_NODENAME}: {reason}")
  try:
    subprocess.run(
      ["scontrol", "update", f"NodeName={SLURMD_NODENAME}", "State=drain", f"Reason=\"{reason}\""],
      check=False, stderr=subprocess.DEVNULL
    )
  except Exception as e:
    print(f"Failed to drain Slurm node {SLURMD_NODENAME}: {e}", file=sys.stderr)

# Undrain the Slurm node
def undrain_node():
  print(f"Undrain Slurm node {SLURMD_NODENAME}")
  try:
    subprocess.run(
      ["scontrol", "update", f"NodeName={SLURMD_NODENAME}", "State=resume"],
      check=False, stderr=subprocess.DEVNULL
    )
  except Exception as e:
    print(f"Failed to undrain Slurm node {SLURMD_NODENAME}: {e}", file=sys.stderr)

# Comment the Slurm node
def comment_node(comment):
  print(f"Comment Slurm node {SLURMD_NODENAME}: {comment}")
  try:
    subprocess.run(
      ["scontrol", "update", f"NodeName={SLURMD_NODENAME}", f"Comment=\"{comment}\""],
      check=False, stderr=subprocess.DEVNULL
    )
  except Exception as e:
    print(f"Failed to comment Slurm node {SLURMD_NODENAME}: {e}", file=sys.stderr)

# Uncomment the Slurm node
def uncomment_node():
  print(f"Uncomment Slurm node {SLURMD_NODENAME}")
  try:
    subprocess.run(
      ["scontrol", "update", f"NodeName={SLURMD_NODENAME}", "Comment=\"\""],
      check=False, stderr=subprocess.DEVNULL
    )
  except Exception as e:
    print(f"Failed to uncomment Slurm node {SLURMD_NODENAME}: {e}", file=sys.stderr)

try:
  if __name__ == "__main__":
    main()
except Exception as e:
  print(f"Unknown error: {e}", file=sys.stderr)
  exit(0)
