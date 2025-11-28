import dataclasses
import json
import os
import pathlib
import subprocess
import sys
import traceback
import typing

# Open the directory from which the checks should be run
def chdir_into_tmp():
  try:
    os.chdir("/tmp")
  except Exception as e:
    print(f"Failed to chdir into /tmp: {e}")
    sys.exit(0)

# TODO: Make it log raw health-checker JSON (multi-line is OK - same as in active checks)
def print_hc_result(
    res_desc: str,
    hc_exitcode: int = -1,
    hc_stdout: str = None,
    hc_stderr: str = None,
):
    print(res_desc)
    print(f"Health checker exit code: {hc_exitcode}")
    print("Health checker stdout:")
    print(hc_stdout)
    print("Health checker stderr:")
    print(hc_stderr)

@dataclasses.dataclass
class HealthCheckerResult:
    json: typing.Optional[dict] = None
    exitcode: int = -1
    stdout: str = ""
    stderr: str = ""
    final_status: str = ""
    first_failed_check: str = ""
    first_failed_error: str = ""

def get_hc_result(proc: subprocess.CompletedProcess) -> HealthCheckerResult:
    res = HealthCheckerResult()
    res.exitcode = proc.returncode
    res.stdout = proc.stdout
    res.stderr = proc.stderr

    try:
        valid_json = None
        for line in proc.stdout.splitlines():
            try:
                obj = json.loads(line)
            except json.decoder.JSONDecodeError:
                continue
            else:
                valid_json = obj

        res.json = valid_json
        res.final_status = res.json.get("status", "")

        for test in res.json.get("tests", []):
            test_name = test.get("name", "")
            for check in test.get("checks", []):
                check_name = check.get("name", "")
                check_state = check.get("state", {})
                if check_state.get("status", "") == "FAIL":
                    res.first_failed_check = f"{test_name}.{check_name}"
                    res.first_failed_error = check_state.get("error", "")
                    return res

        return res
    except Exception:
        return res
    
def ensure_output_dir(path_str: str):
    path = pathlib.Path(path_str)

    old_umask = os.umask(0)
    try:
        path.mkdir(parents=True, exist_ok=True)
        os.chmod(path, 0o777)
    finally:
        os.umask(old_umask)

try:
    # Get environment variables
    try:
        CHECKS_PLATFORM_TAG = os.environ["CHECKS_PLATFORM_TAG"]
        CHECKS_CONTEXT = os.environ["CHECKS_CONTEXT"]
    except KeyError as ke:
        print(f"Failed to get environment variable '{ke.args[0]}'")
        sys.exit(0)

    # Change into /tmp before running health-checker
    chdir_into_tmp()

    # Define tests to run
    tests: str = ""
    if CHECKS_CONTEXT == "prolog":
        tests = "module,nvidia_smi,nvidia_smi_nvlink,nvidia_smi_topo,dmesg,ib_link"
    elif CHECKS_CONTEXT == "epilog":
        tests = "module,nvidia_smi,nvidia_smi_nvlink,nvidia_smi_topo,dcgmi_diag_r1,dmesg,ib_link"
    elif CHECKS_CONTEXT == "hc_program":
        tests = "module,nvidia_smi,nvidia_smi_nvlink,nvidia_smi_topo,dmesg,ib_link"
    else:
        print(f"Unknown context '{CHECKS_CONTEXT}'")
        sys.exit(0)

    # Set custom options for health-checker
    env = os.environ.copy()
    env["HC_DCGMI_DIAG_R1_DEBUGLOGFILE"] = "/dev/null"
    env["HC_DCGMI_DIAG_R1_DEBUGLEVEL"] = "NONE"

    output_dir = "/opt/soperator-outputs/health_checker_cmd_stdout"
    ensure_output_dir(output_dir)
    # Run Nebius GPU health-checker
    cmd = [
        "health-checker", "run",
        "-e", "soperator",
        "-p", CHECKS_PLATFORM_TAG,
        "-n", tests,
        "-f", "json-partial",
        "--tests-stdout-path", output_dir,
        "--log-level", "info",
    ]
    proc = subprocess.run(cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE, universal_newlines=True, env=env)
    result = get_hc_result(proc)
    #print(result)

    if result.exitcode == 0:
        if result.final_status == "PASS":
            print_hc_result(
                res_desc="All checks passed",
                hc_exitcode=result.exitcode,
                hc_stdout=result.stdout,
                hc_stderr=result.stderr,
            )
            sys.exit(0)
        elif result.final_status == "FAIL" and result.first_failed_check:
            print_hc_result(
                res_desc="Some checks failed",
                hc_exitcode=result.exitcode,
                hc_stdout=result.stdout,
                hc_stderr=result.stderr,
            )
            # Return details with the first failed check
            details=result.first_failed_check
            if result.first_failed_error:
                details=f"{result.first_failed_check}: {result.first_failed_error}"
            os.write(3, details.encode("utf-8", errors="backslashreplace"))
            sys.exit(1)
        elif result.final_status == "ERROR":
            print_hc_result(
                res_desc="Error when running checks",
                hc_exitcode=result.exitcode,
                hc_stdout=result.stdout,
                hc_stderr=result.stderr,
            )
            sys.exit(0)

    print_hc_result(
        res_desc="Unexpected result",
        hc_exitcode=result.exitcode,
        hc_stdout=result.stdout,
        hc_stderr=result.stderr,
    )
    sys.exit(0)

except Exception:
    print("Unhandled exception")
    traceback.print_exc()
    sys.exit(0)
