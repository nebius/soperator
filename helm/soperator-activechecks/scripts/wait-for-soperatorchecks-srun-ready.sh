#!/bin/bash
set -euxo pipefail

MAX_ATTEMPTS=120
SLEEP_INTERVAL=2

# Link users from jail. The slurm_check_job image deletes /etc/passwd* files and normally
# links them via its entrypoint. However, since we override the entrypoint with a custom
# command, we must do the linking here for runuser to resolve the soperatorchecks user.
ln -sf /mnt/jail/etc/passwd /etc/passwd
ln -sf /mnt/jail/etc/group /etc/group
ln -sf /mnt/jail/etc/shadow /etc/shadow
ln -sf /mnt/jail/etc/gshadow /etc/gshadow
chown -h 0:42 /etc/{shadow,gshadow} || true

# Symlink slurm configs from jail so srun can find slurm.conf at /etc/slurm/
# Must remove directory first - ln -sf won't replace a directory with a symlink
rm -rf /etc/slurm && ln -s /mnt/jail/etc/slurm /etc/slurm

echo "Waiting for Slurm controller to be ready for soperatorchecks user..."

for i in $(seq 1 "$MAX_ATTEMPTS"); do
  echo "Attempt $i/$MAX_ATTEMPTS: Testing srun availability..."
  if runuser -u soperatorchecks -- srun --mpi=none --job-name=test-controller-is-ready -n1 -t1 --partition=hidden hostname 2>&1; then
    echo "SUCCESS: Slurm controller is ready for soperatorchecks user"
    exit 0
  fi
  echo "Attempt $i/$MAX_ATTEMPTS failed, waiting $SLEEP_INTERVAL seconds..."
  sleep "$SLEEP_INTERVAL"
done

echo "ERROR: Failed to verify Slurm readiness after $MAX_ATTEMPTS attempts"
exit 1
