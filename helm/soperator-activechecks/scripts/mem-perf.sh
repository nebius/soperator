#!/bin/bash
#SBATCH --deadline="now+6hours"
#SBATCH --time=00:05:00
#SBATCH --exclusive

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
echo "Running mem_perf check on $(hostname)..."
HC_OUTPUT=$(srun --container-image={{ .Values.activeCheckImage }} \
  --container-mounts=$(which health-checker):/usr/local/bin/health-checker --cpu-bind=verbose,cores \
  bash -c "health-checker run -e soperator -p $platform -n mem_bw,mem_lat -f json-partial --tests-stdout-path /opt/soperator-outputs/health_checker_cmd_stdout")
HC_EXIT_CODE=$?

echo "Health checker output: $HC_OUTPUT"
echo "Health checker job step exit code: $HC_EXIT_CODE"
HC_STATUS=$(echo "$HC_OUTPUT" | awk '/^\s*{/,/^\s*}/' | jq -r '.status')

echo "Health checker status: $HC_STATUS"
if [[ "$HC_STATUS" == "FAIL" ]]; then
  echo "Health-checker reported status=FAIL."
  exit 1
elif [[ "$HC_STATUS" == "ERROR" ]]; then
  echo "Health-checker reported status=ERROR."
  exit 0
else
  echo "Health-checker passed or returned non-FAIL status."
  exit 0
fi
