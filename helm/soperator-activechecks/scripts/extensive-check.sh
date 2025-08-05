#!/bin/bash
#SBATCH --deadline="now+6hours"
#SBATCH --time=10:00
#SBATCH --mem=32G
#SBATCH --gpus-per-node=1
#SBATCH --cpus-per-task=2

echo "Running extensive health check..."

sleep 3600 # 1 hour

HEALTH_CHECK_PASSED=$(($RANDOM % 2))
echo "HEALTH_CHECK_PASSED=$HEALTH_CHECK_PASSED"

if [[ $HEALTH_CHECK_PASSED -eq 1 ]]; then
  echo "Health-checker passed or returned non-ERROR status."
  exit 0
else
  echo "Health-checker reported status=ERROR and exited with non-zero status."
  exit 1
fi