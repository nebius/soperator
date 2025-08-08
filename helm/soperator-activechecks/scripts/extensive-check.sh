#!/bin/bash
#SBATCH --deadline="now+6hours"
#SBATCH --time=10:00
#SBATCH --mem=32G
#SBATCH --gpus-per-node=8
#SBATCH --cpus-per-task=16

echo "Running extensive health check..."

sleep 10

HEALTH_CHECK_PASSED=$(RANDOM % 2)

if [[ $HEALTH_CHECK_PASSED -eq 1 ]]; then
  echo "Health-checker passed or returned non-ERROR status."
  exit 0
else
  echo "Health-checker reported status=ERROR and exited with non-zero status."
  exit 1
fi
