#!/bin/bash
set -e

MAX_ATTEMPTS=60

echo "Waiting for Slurm controller to be ready for soperatorchecks user..."

for i in $(seq 1 "$MAX_ATTEMPTS"); do
  echo "Attempt $i/$MAX_ATTEMPTS: Testing srun availability..."
  if srun --job-name=test-controller-is-ready -n1 -t1 hostname; then
    echo "SUCCESS: Slurm controller is ready for soperatorchecks user"
    exit 0
  fi
  echo "Attempt $i/$MAX_ATTEMPTS failed, waiting 1 second..."
  sleep 1
done

echo "ERROR: Failed to verify Slurm readiness after $MAX_ATTEMPTS attempts"
exit 1
