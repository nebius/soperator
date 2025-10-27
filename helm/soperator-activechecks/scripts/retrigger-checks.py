import os
import subprocess
import logging
import time
import json
import datetime

NS = os.environ["NAMESPACE"]

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
        rac = it.get("spec", {}).get("runAfterCreation")
        if rac:
            active_checks.append(it["metadata"]["name"])
    return active_checks

def cronjob_exists(name: str) -> bool:
    return subprocess.run(
        ["kubectl", "-n", NS, "get", "cronjob", name],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL
    ).returncode == 0

def trigger(name: str):
    ts = datetime.datetime.now().strftime("%Y%m%d%H%M%S")
    job = f"{name}-manual-{ts}"
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
