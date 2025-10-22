import os
import sys
import subprocess
import json
import datetime

NS = os.environ.get("NAMESPACE")

def fail(msg: str, code: int = 1):
    print(msg, file=sys.stderr)
    sys.exit(code)

if not NS:
    fail("missing NAMESPACE var")

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
    res = []
    for it in data.get("items", []):
        rac = it.get("spec", {}).get("runAfterCreation")
        if rac is True or str(rac).lower() == "true":
            res.append(it["metadata"]["name"])
    return res

def cronjob_exists(name: str) -> bool:
    return subprocess.run(
        ["kubectl", "-n", NS, "get", "cronjob", name],
        stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL
    ).returncode == 0

def trigger(name: str):
    ts = datetime.datetime.now().strftime("%Y%m%d%H%M%S")
    job = f"{name}-manual-{ts}"
    cmd = ["kubectl", "-n", NS, "create", "job", f"--from=cronjob/{name}", job]
    print(f"Trigger {NS}/{name} -> {job}")
    run(cmd)

def main():
    activechecks = get_active_checks()
    if not activechecks:
        print("No CRs with .spec.runAfterCreation=true"); return

    for name in activechecks:
        if cronjob_exists(name):
            try:
                trigger(name)
            except Exception as e:
                print(f"❌ {NS}/{name}: {e}")
        else:
            print(f"⚠️  CronJob not found: {NS}/{name}")

if __name__ == "__main__":
    main()
