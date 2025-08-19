import dataclasses
import json
import os
import subprocess
import sys
import traceback
import typing

def log_json(
    res_desc: str,
    hc_json: typing.Any = None,
    hc_exitcode: int = -1,
    hc_stdout: str = None,
    hc_stderr: str = None,
):
    payload = {"result_description": res_desc}
    if hc_json:
        payload["health_checker_json"] = hc_json
    if hc_exitcode != -1:
        payload["health_checker_exitcode"] = str(hc_exitcode)
    if hc_stdout:
        payload["health_checker_stdout"] = hc_stdout
    if hc_stderr:
        payload["health_checker_stderr"] = hc_stderr
    try:
        json_oneline = json.dumps(payload, separators=(",", ":"), ensure_ascii=False)
    except Exception:
        json_oneline = '{"result_description": "Failed to dump JSON log"}'
    # Print a single-line JSON
    print(json_oneline)

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
    res.stderr = proc.stderr

    try:
        valid_json = None
        for line in proc.stdout.splitlines():
            try:
                obj = json.loads(line)
            except json.decoder.JSONDecodeError:
                res.stdout += line + "\n"
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

try:
    # Get environment variables
    try:
        CHECKS_PLATFORM_TAG = os.environ["CHECKS_PLATFORM_TAG"]
        CHECKS_CONTEXT = os.environ["CHECKS_CONTEXT"]
    except KeyError as ke:
        log_json(res_desc=f"Failed to get environment variable '{ke.args[0]}'")
        sys.exit(0)

    # Define tests to run
    tests: str = ""
    if CHECKS_CONTEXT == "prolog":
        tests = "module,nvidia_smi,nvidia_smi_nvlink,nvidia_smi_topo,dmesg,ib_link"
    elif CHECKS_CONTEXT == "epilog":
        tests = "module,nvidia_smi,nvidia_smi_nvlink,nvidia_smi_topo,dcgmi_diag_r1,dmesg,ib_link"
    elif CHECKS_CONTEXT == "hc_program":
        tests = "module,nvidia_smi,nvidia_smi_nvlink,nvidia_smi_topo,dmesg,ib_link"
    else:
        log_json(res_desc=f"Unknown context '{CHECKS_CONTEXT}'")
        sys.exit(0)

    # Set custom options for health-checker
    env = os.environ.copy()
    env["HC_DCGMI_DIAG_R1_DEBUGLOGFILE"] = "/dev/null"
    env["HC_DCGMI_DIAG_R1_DEBUGLEVEL"] = "NONE"

    # Run Nebius GPU health-checker
    cmd = [
        "health-checker", "run",
        "-e", "soperator",
        "-p", CHECKS_PLATFORM_TAG,
        "-n", tests,
        "-f", "json-partial",
        "--tests-stdout-path", "/opt/soperator-outputs/health_checker_cmd_stdout",
        "--log-level", "info",
    ]
    proc = subprocess.run(cmd, stdout=subprocess.PIPE, stderr=subprocess.PIPE, universal_newlines=True, env=env)
    result = get_hc_result(proc)
    #print(result)

    if result.exitcode == 0:
        if result.final_status == "PASS":
            log_json(res_desc="All checks passed", hc_json=result.json)
            sys.exit(0)
        elif result.final_status == "FAIL" and result.first_failed_check:
            log_json(res_desc="Some checks failed", hc_json=result.json)
            # Return details with the first failed check
            details=result.first_failed_check
            if result.first_failed_error:
                details=f"{result.first_failed_check}: {result.first_failed_error}"
            os.write(3, details.encode("utf-8", errors="backslashreplace"))
            sys.exit(1)
        elif result.final_status == "ERROR":
            log_json(res_desc="Error when running checks", hc_json=result.json, hc_stdout=result.stdout, hc_stderr=result.stderr)
            sys.exit(0)

    log_json(
        res_desc="Unexpected result",
        hc_json=result.json,
        hc_exitcode=result.exitcode,
        hc_stdout=result.stdout,
        hc_stderr=result.stderr,
    )
    sys.exit(0)

except Exception:
    log_json(res_desc="Unhandled exception")
    #traceback.print_exc()
    exit(0)
