import os
import subprocess
import logging
import time
import json
import datetime
import hashlib

NS = os.environ["NAMESPACE"]
SLURM_CLUSTER_REF_NAME = os.environ["SLURM_CLUSTER_REF_NAME"]
K8S_LABEL_VALUE_MAX_LENGTH = 63

logging.Formatter.converter = time.gmtime
logging.basicConfig(
    format='[%(asctime)s.%(msecs)03d UTC] %(levelname)s: %(message)s',
    datefmt='%Y-%m-%d %H:%M:%S',
    level=logging.INFO
)

def run(cmd):
    p = subprocess.run(cmd, capture_output=True, text=True)
    if p.returncode != 0:
        raise RuntimeError(f"cmd failed: {' '.join(cmd)}\n{p.stderr}")
    return p.stdout.strip()

def get_active_checks():
    data = json.loads(run([
        "kubectl", "get", "activechecks.slurm.nebius.ai",
        "-n", NS, "-o", "json"
    ]))
    active_checks = []
    for it in data.get("items", []):
        spec = it.get("spec", {})
        if spec.get("runAfterCreation") and spec.get("slurmClusterRefName") == SLURM_CLUSTER_REF_NAME:
            active_checks.append(it["metadata"]["name"])
    return active_checks

def cronjob_exists(name: str) -> bool:
    return subprocess.run(
        ["kubectl", "-n", NS, "get", "cronjob", name],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL
    ).returncode == 0

def manual_job_name(name: str) -> str:
    ts = datetime.datetime.now(datetime.timezone.utc).strftime("%Y%m%d%H%M%S")
    suffix = f"-manual-{ts}"
    candidate = f"{name}{suffix}"
    if len(candidate) <= K8S_LABEL_VALUE_MAX_LENGTH:
        return candidate

    digest = hashlib.sha1(name.encode()).hexdigest()[:8]
    hash_suffix = f"-{digest}"
    prefix_length = K8S_LABEL_VALUE_MAX_LENGTH - len(hash_suffix) - len(suffix)
    prefix = name[:prefix_length].rstrip("-") or "check"
    return f"{prefix}{hash_suffix}{suffix}"

def trigger(name: str):
    job = manual_job_name(name)
    cmd = ["kubectl", "-n", NS, "create", "job", f"--from=cronjob/{name}", job]
    logging.info(f"Triggering {NS}/{name} -> {job}")
    run(cmd)

def main():
    active_checks = get_active_checks()
    if not active_checks:
        logging.info("No CRs with .spec.runAfterCreation=true")
        return

    for name in active_checks:
        if cronjob_exists(name):
            try:
                trigger(name)
            except RuntimeError as e:
                logging.error(f"{NS}/{name}: {e}")
        else:
            logging.warning(f"CronJob not found: {NS}/{name}")

if __name__ == "__main__":
    main()
