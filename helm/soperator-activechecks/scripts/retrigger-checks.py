import subprocess, json, datetime

def run(cmd):
  p = subprocess.run(cmd, capture_output=True, text=True)
  if p.returncode != 0:
    raise RuntimeError(f"cmd failed: {' '.join(cmd)}\n{p.stderr}")
  return p.stdout.strip()

def get_active_checks():
  data = json.loads(run(["kubectl", "get", "activechecks.slurm.nebius.ai", "-n", "soperator", "-o", "json"]))
  res = []
  for it in data.get("items", []):
    rac = it.get("spec", {}).get("runAfterCreation")
    if rac is True or str(rac).lower() == "true":
      res.append((it["metadata"]["namespace"], it["metadata"]["name"]))
  return res

def cronjob_exists(ns, name):
  return subprocess.run(
    ["kubectl", "-n", ns, "get", "cronjob", name],
    stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL
  ).returncode == 0

def trigger(ns, name):
  ts = datetime.datetime.now().strftime("%Y%m%d%H%M%S")
  job = f"{name}-manual-{ts}"
  cmd = ["kubectl", "-n", ns, "create", "job", f"--from=cronjob/{name}", job]
  print(f"Trigger {ns}/{name} -> {job}")
  run(cmd)

def main():
  activechecks = get_active_checks()
  if not activechecks:
    print("No CRs with .spec.runAfterCreation=true"); return

  for ns, name in activechecks:
    if cronjob_exists(ns, name):
      try: trigger(ns, name)
      except Exception as e: print(f"❌ {ns}/{name}: {e}")
    else:
      print(f"⚠️  CronJob not found: {ns}/{name}")

if __name__ == "__main__":
  main()
