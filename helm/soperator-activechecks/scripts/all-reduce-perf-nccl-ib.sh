#!/bin/bash
#SBATCH --deadline="now+6hours"
#SBATCH --time=10:00
#SBATCH --mem=32G
#SBATCH --gpus-per-node=8
#SBATCH --cpus-per-task=16

echo "Checking for running GPU processes..."
if [[ -n "$(nvidia-smi --query-compute-apps=pid --format=csv,noheader | grep -v '^ *$')" ]]; then
  echo "Another GPU process is currently running. Exiting."
  exit 0
fi

platform=""
gpus_on_node=$(nvidia-smi --query-gpu=name --format=csv,noheader | sort | uniq -c)
if [[ "${gpus_on_node}" == *"8 NVIDIA H100"* ]]; then
  platform="8xH100"
elif [[ "${gpus_on_node}" == *"8 NVIDIA H200"* ]]; then
  platform="8xH200"
elif [[ "${gpus_on_node}" == *"8 NVIDIA B200"* ]]; then
  platform="8xB200"
else
  echo "Unsupported platform"
  exit 0
fi

echo "Platform found: $platform"
echo "Running all_reduce_with_ib check on $(hostname)..."
HC_OUTPUT=$(srun --cpu-bind=verbose,cores bash -c "health-checker run -e soperator -p $platform -n all_reduce_with_ib --json-log")
HC_EXIT_CODE=$?

echo "Health checker output: $HC_OUTPUT"
echo "Health checker job step exit code: $HC_EXIT_CODE"
HC_STATUS=$(echo "$HC_OUTPUT" | awk '/^\s*{/,/^\s*}/' | jq -r '.status')

echo "Health checker status: $HC_STATUS"
if [[ "$HC_STATUS" == "ERROR" && $HC_EXIT_CODE -eq 1 ]]; then
  echo "Health-checker reported status=ERROR and exited with non-zero status."
  exit 1
else
  echo "Health-checker passed or returned non-ERROR status."
  exit 0
fi
