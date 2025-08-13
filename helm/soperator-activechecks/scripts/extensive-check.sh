#!/bin/bash
#SBATCH --deadline="now+6hours"
#SBATCH --time=10:00
#SBATCH --mem=32G
#SBATCH --gpus-per-node=1
#SBATCH --cpus-per-task=2

echo "Running extensive health check..."

sleep 30

# Always fail in order not to remove the reservation until we implement a real extensive check
HEALTH_CHECK_PASSED=$((0))

# For testing: use this line to randomly fail or succeed
# HEALTH_CHECK_PASSED=$(($RANDOM % 2))

echo "HEALTH_CHECK_PASSED=$HEALTH_CHECK_PASSED"

if [[ $HEALTH_CHECK_PASSED -eq 1 ]]; then
  echo "Health-checker passed or returned non-ERROR status."
  exit 0
else
  echo "Health-checker reported status=ERROR and exited with non-zero status."
  exit 1
fi