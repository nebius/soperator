import functools
import json
import logging
import os
import re
import string
import subprocess
import sys
import time
import typing

SOPERATOR_NODE_METADATA_FILE = "/run/soperator/node_metadata.env"
SOPERATOR_NODE_REAL_MEMORY_BYTES = "SOPERATOR_NODE_REAL_MEMORY_BYTES"
SOPERATOR_NODE_PLATFORM_TAG = "SOPERATOR_NODE_PLATFORM_TAG"
SOPERATOR_NODE_PLATFORM_TAGS = "SOPERATOR_NODE_PLATFORM_TAGS"

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
    # - "any" - run on any platform
    # - "CPU" - run on nodes without GPUs
    # - "<num>xGPU" - run on nodes with <num> GPUs of any model
    # - "<num>x<gpu_model>" - run on nodes with <num> GPUs of model <gpu_model>,
    #   taken from the Gres type in uppercase, e.g. "H100" for "Gres=gpu:nvidia_h100_80gb_hbm3:8"
    platforms: list[str] = ["any"]

    # Whether to skip this check for jobs that don't allocate any GPUs.
    # Allows to skip the check for CPU-only jobs in "prolog" and "epilog" contexts even if the node is equipped with GPUs.
    skip_for_cpu_jobs: bool = False

    # Whether to skip this check for CPU-only jobs that don't allocate all effective CPUs on this node.
    # Applies in "prolog" and "epilog"; also skips when CPU allocation data is unavailable.
    skip_for_partial_cpu_jobs: bool = False

    # Whether to skip this check for jobs that don't allocate all available GPUs.
    # CPU-only jobs are not considered "partial GPU"
    skip_for_partial_gpu_jobs: bool = False

    # What contexts this check should run in.
    # Supported values:
    # - "any" - any context
    # - "none" - never run
    # - "prolog" - run in Slurm job Prolog script (on each node, before the job)
    # - "epilog" - run in Slurm job Epilog script (on each node, after the job)
    # - "hc_program" - run in Slurm HealthCheckProgram script (on each node, periodically)
    contexts: list[str] = ["any"]

    # Nodes in what states this check should run on.
    # Supported values:
    # - "any" - run on nodes in any state and skip state detection
    # - "drain" - run on drained/draining nodes
    node_states: list[str] = ["any"]

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
    # - CHECKS_NODE_STATE_FLAGS - "+"-separated list of Slurm node state flags
    # - CHECKS_NODE_REASON - (drain/down) reason field of the Slurm node
    # - CHECKS_NODE_COMMENT - comment field of the Slurm node
    # These values require Slurm queries, so they aren't exported by default.
    need_env: list[str] = []

class NodeInfo(typing.NamedTuple):
    state_flags: list[str] = []
    reason: str = ""
    comment: str = ""
    effective_cpus: int = 0

def read_node_metadata(key: typing.Optional[str] = None) -> typing.Union[dict[str, str], str]:
    """Read all metadata variables, or return a single variable by key."""
    metadata = {}
    with open(SOPERATOR_NODE_METADATA_FILE, encoding="utf-8") as metadata_file:
        for raw_line in metadata_file:
            line = raw_line.rstrip("\n")
            if not line or line.startswith("#"):
                continue
            name, separator, value = line.partition("=")
            if not separator or not re.fullmatch(r"[A-Za-z_][A-Za-z0-9_]*", name) or "\0" in value:
                raise ValueError(f"invalid metadata entry in {SOPERATOR_NODE_METADATA_FILE}")
            if name in metadata:
                raise ValueError(f"duplicate {name} in {SOPERATOR_NODE_METADATA_FILE}")
            metadata[name] = value
    return metadata if key is None else metadata[key]


# Load node metadata into the process environment.
try:
    node_metadata = read_node_metadata()
    for key in ("NODESET_GPU_ENABLED", SOPERATOR_NODE_PLATFORM_TAG, SOPERATOR_NODE_PLATFORM_TAGS, SOPERATOR_NODE_REAL_MEMORY_BYTES):
        if key not in node_metadata:
            raise ValueError(f"missing {key} in {SOPERATOR_NODE_METADATA_FILE}")
    os.environ.update(node_metadata)
    os.environ["CHECKS_PLATFORM_TAG"] = os.environ[SOPERATOR_NODE_PLATFORM_TAG]
    os.environ["CHECKS_PLATFORM_TAGS"] = os.environ[SOPERATOR_NODE_PLATFORM_TAGS]
    os.environ["CHECKS_NODE_REAL_MEM_BYTES"] = os.environ[SOPERATOR_NODE_REAL_MEMORY_BYTES]
except (OSError, ValueError) as error:
    logging.error(f"Read node metadata: {error}; skipping checks")
    sys.exit(0)

# Get environment variables
try:
    SLURMD_NODENAME = os.environ["SLURMD_NODENAME"]
    SLURM_JOB_ID = os.environ.get("SLURM_JOB_ID", "") # Not available in the "hc_program" context
    SLURM_JOB_GPUS = os.environ.get("SLURM_JOB_GPUS", "") # Not available in the "hc_program context"
    SLURM_JOB_CPUS_PER_NODE = os.environ.get("SLURM_JOB_CPUS_PER_NODE", "")
    SLURM_JOB_NODELIST = os.environ.get("SLURM_JOB_NODELIST", "")
    SLURM_JOB_COMMENT = os.environ.get("SLURM_JOB_COMMENT", "") # Not available in the "hc_program context"
    CHECKS_OUTPUTS_BASE_DIR = os.environ["CHECKS_OUTPUTS_BASE_DIR"]
    CHECKS_CONTEXT = os.environ["CHECKS_CONTEXT"]
    CHECKS_CONFIG = os.environ["CHECKS_CONFIG"]
    CHECKS_PLATFORM_TAG = os.environ["CHECKS_PLATFORM_TAG"]
    CHECKS_PLATFORM_TAGS = os.environ["CHECKS_PLATFORM_TAGS"].split(",")
    CHECKS_NODE_REAL_MEM_BYTES = int(os.environ["CHECKS_NODE_REAL_MEM_BYTES"])
    if not re.fullmatch(r"[0-9]+", os.environ["CHECKS_NODE_REAL_MEM_BYTES"]) or CHECKS_NODE_REAL_MEM_BYTES <= 0:
        raise ValueError("invalid CHECKS_NODE_REAL_MEM_BYTES")
    if os.environ["NODESET_GPU_ENABLED"] == "false":
        if CHECKS_PLATFORM_TAG != "CPU" or CHECKS_PLATFORM_TAGS != ["CPU"]:
            raise ValueError("invalid CPU platform metadata")
    elif os.environ["NODESET_GPU_ENABLED"] == "true":
        match = re.fullmatch(r"([1-9][0-9]*)x([A-Z0-9]+)", CHECKS_PLATFORM_TAG)
        if not match:
            raise ValueError("invalid CHECKS_PLATFORM_TAG on a GPU worker")
        expected_tags = [CHECKS_PLATFORM_TAG] if match[2] == "GPU" else [CHECKS_PLATFORM_TAG, f"{match[1]}xGPU"]
        if CHECKS_PLATFORM_TAGS != expected_tags:
            raise ValueError("inconsistent CHECKS_PLATFORM_TAGS")
    else:
        raise ValueError("invalid NODESET_GPU_ENABLED")
except (KeyError, ValueError) as error:
    logging.error(f"Initialize check runner environment: {error}; skipping checks")
    sys.exit(0)

def main():
    start_time = time.perf_counter()
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
        sys.exit(0)

    # Load checks from a config file
    try:
        with open(CHECKS_CONFIG, encoding="utf-8") as f:
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

    end_time = time.perf_counter()
    logging.info(f"Finished in {end_time - start_time:.3f} seconds")
    sys.exit(0)

# Filter checks for the current environment
def filter_applicable_checks(checks: list[Check]) -> list[Check]:
    # Filter by context
    checks = filter_by_context(checks)
    # Filter by skip_for_cpu_jobs
    checks = filter_by_skip_for_cpu_jobs(checks)
    # Filter by platform (needs platform tags)
    checks = filter_by_platform(checks)
    # Filter by skip_for_partial_gpu_jobs (needs platform tags)
    checks = filter_by_skip_for_partial_gpu_jobs(checks)
    checks = filter_by_skip_for_partial_cpu_jobs(checks)
    # Filter by node_state (needs node info)
    checks = filter_by_node_state(checks)
    return checks

def filter_by_context(checks: list[Check]) -> list[Check]:
    # Skip if all checks don't care
    if all("any" in check.contexts for check in checks):
        return checks
    return [
        check for check in checks
        if (
            "any" in check.contexts or
            CHECKS_CONTEXT in check.contexts
        )
    ]

def filter_by_skip_for_cpu_jobs(checks: list[Check]) -> list[Check]:
    # Skip if all checks don't care
    if all(not check.skip_for_cpu_jobs for check in checks):
        return checks
    # Skip for non-job-related runs
    if not job_related_run():
        return checks
    num_gpus = get_job_alloc_gpus()
    return [
        check for check in checks
        if not (check.skip_for_cpu_jobs and num_gpus == 0)
    ]

def filter_by_platform(checks: list[Check]) -> list[Check]:
    # Skip if all checks don't care
    if all("any" in check.platforms for check in checks):
        return checks
    return [
        check for check in checks
        if (
            "any" in check.platforms or
            any(tag in check.platforms for tag in CHECKS_PLATFORM_TAGS)
        )
    ]

def filter_by_skip_for_partial_gpu_jobs(checks: list[Check]) -> list[Check]:
    # Skip if all checks don't care
    if all(not check.skip_for_partial_gpu_jobs for check in checks):
        return checks
    # Skip for non-job-related runs
    if not job_related_run():
        return checks
    job_alloc_gpus = get_job_alloc_gpus()
    return [
        check for check in checks
        if not (
            check.skip_for_partial_gpu_jobs and
            job_alloc_gpus > 0 and
            f"{job_alloc_gpus}xGPU" not in CHECKS_PLATFORM_TAGS
        )
    ]

def filter_by_skip_for_partial_cpu_jobs(checks: list[Check]) -> list[Check]:
    if all(not check.skip_for_partial_cpu_jobs for check in checks):
        return checks
    if not job_related_run() or get_job_alloc_gpus() > 0:
        return checks

    job_alloc_cpus = get_job_alloc_cpus()
    node_effective_cpus = get_node_info().effective_cpus
    if (
        type(job_alloc_cpus) is not int or type(node_effective_cpus) is not int or
        job_alloc_cpus <= 0 or node_effective_cpus <= 0
    ):
        logging.warning(
            f"Skipping checks with skip_for_partial_cpu_jobs for job {SLURM_JOB_ID} on node {SLURMD_NODENAME}: "
            f"CPU allocation data is unavailable or invalid (job CPUs={job_alloc_cpus}, effective CPUs={node_effective_cpus})"
        )
    elif job_alloc_cpus < node_effective_cpus:
        logging.info(
            f"Skipping checks with skip_for_partial_cpu_jobs for job {SLURM_JOB_ID} on node {SLURMD_NODENAME}: "
            f"Job allocates {job_alloc_cpus} of {node_effective_cpus} effective CPUs"
        )
    else:
        return checks

    return [check for check in checks if not check.skip_for_partial_cpu_jobs]

def filter_by_node_state(checks: list[Check]) -> list[Check]:
    # Skip if all checks don't care
    if all("any" in check.node_states for check in checks):
        return checks
    node_info = get_node_info()
    return [
        check for check in checks
        if (
            "any" in check.node_states or
            ("drain" in check.node_states and "DRAIN" in node_info.state_flags)
        )
    ]

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
    start_time = time.perf_counter()
    logging.info(f"Running check {check.name} ({check.command}), logging to {log_abs_path}")
    logging.info(f"Check spec: {json.dumps(check._asdict(), indent=2)}")

    # Create parent dirs with full permissions
    os.makedirs(os.path.dirname(log_abs_path), mode=0o777, exist_ok=True)

    # Execute the check command
    cmd = ["bash", "-l", "-c", f"{check.command} 3>&1 1>\"{log_abs_path}\" 2>&1"]
    result = subprocess.run(cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True)

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
    end_time = time.perf_counter()
    logging.info(f"Completed check {check.name} in {end_time - start_time:.3f} seconds")

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

    # Please note that "undrain" and "uncomment" actions can be issues only from "hc_program" context.
    if check.on_ok in ("undrain", "uncomment") and CHECKS_CONTEXT != "hc_program":
        logging.info(f"Skipping on_ok={check.on_ok} in unsupported context {CHECKS_CONTEXT}")
        return

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
        if env == "CHECKS_NODE_STATE_FLAGS":
            os.environ["CHECKS_NODE_STATE_FLAGS"] = "+".join(get_node_info().state_flags)
        if env == "CHECKS_NODE_REASON":
            os.environ["CHECKS_NODE_REASON"] = get_node_info().reason
        if env == "CHECKS_NODE_COMMENT":
            os.environ["CHECKS_NODE_COMMENT"] = get_node_info().comment

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
        info = NodeInfo(
            state_flags=node.get("state", []),
            reason=node.get("reason", ""),
            comment=node.get("comment", ""),
            effective_cpus=node.get("effective_cpus", 0)
        )
        logging.info(f"Slurm node info: {json.dumps(info._asdict(), indent=2)}")
        return info
    except Exception as e:
        logging.warning(f"Failed to get info about Slurm node {SLURMD_NODENAME}: {e}")
        return NodeInfo()

def job_related_run() -> bool:
    return CHECKS_CONTEXT in ("prolog", "epilog")

def get_job_alloc_gpus() -> int:
    if SLURM_JOB_GPUS == "":
        return 0
    return len(SLURM_JOB_GPUS.split(","))

# SLURM_JOB_CPUS_PER_NODE is ordered like SLURM_JOB_NODELIST, e.g. "64(x2),32".
@functools.lru_cache(maxsize=1)
def get_job_alloc_cpus() -> int:
    try:
        cpu_runs = []
        for entry in SLURM_JOB_CPUS_PER_NODE.split(","):
            match = re.fullmatch(r"([1-9][0-9]*)(?:\(x([1-9][0-9]*)\))?", entry.strip())
            if not match:
                raise ValueError("parse SLURM_JOB_CPUS_PER_NODE")
            cpu_runs.append((int(match[1]), int(match[2] or "1")))
        if not SLURM_JOB_NODELIST:
            raise ValueError("read SLURM_JOB_NODELIST")

        nodes = expand_hostlist(SLURM_JOB_NODELIST)
        if len(nodes) != sum(repeats for _, repeats in cpu_runs) or nodes.count(SLURMD_NODENAME) != 1:
            raise ValueError("match CPU allocation to the current node")
        node_index = nodes.index(SLURMD_NODENAME)
        for cpus, repeats in cpu_runs:
            if node_index < repeats:
                return cpus
            node_index -= repeats
    except Exception as e:
        logging.warning(f"Read CPU allocation from job environment for node {SLURMD_NODENAME}: {e}")
    return 0

def expand_hostlist(expression: str) -> list[str]:
    # Feature expressions need a controller RPC; job node lists contain concrete node names and ranges.
    if "{" in expression or "}" in expression:
        raise ValueError("expand node list without querying slurmctld")
    if "[" not in expression and "]" not in expression:
        return expression.split(",")
    result = subprocess.run(
        ["scontrol", "show", "hostnames", expression],
        check=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, timeout=10
    )
    return result.stdout.splitlines()

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
            ["scontrol", "update", f"NodeName={SLURMD_NODENAME}", "State=drain", f"Reason={reason}"],
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
            ["scontrol", "update", f"NodeName={SLURMD_NODENAME}", f"Comment={comment}"],
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
            ["scontrol", "update", f"NodeName={SLURMD_NODENAME}", "Comment="],
            check=False, stderr=subprocess.DEVNULL
        )
        # Invalidate cache for the Slurm node info
        get_node_info.cache_clear()
    except Exception as e:
        logging.warning(f"Failed to uncomment Slurm node {SLURMD_NODENAME}: {e}")

try:
    if __name__ == "__main__":
        main()
except Exception:
    logging.exception("Unknown error")
    sys.exit(0)
