#!/bin/bash
#SBATCH --deadline="now+4hours"
#SBATCH --time=10:00
#SBATCH --gpus-per-node=8
#SBATCH --exclusive
#SBATCH --mem=0

platform=""
gpus_on_node=$(nvidia-smi --query-gpu=name --format=csv,noheader | sort | uniq -c)
if [[ "${gpus_on_node}" == *"8 NVIDIA H100"* ]]; then
  platform="8xH100"
elif [[ "${gpus_on_node}" == *"8 NVIDIA H200"* ]]; then
  platform="8xH200"
elif [[ "${gpus_on_node}" == *"8 NVIDIA B200"* ]]; then
  platform="8xB200"
elif [[ "${gpus_on_node}" == *"8 NVIDIA B300"* ]]; then
  platform="8xB300"
else
  echo "Unsupported platform"
  exit 0
fi

echo "Platform found: $platform"
echo "Running cuda samples check on $(hostname)..."
HC_OUTPUT_DIR="/opt/soperator-outputs/health_checker_cmd_stdout"
HC_OUTPUT=$(srun --cpu-bind=verbose --container-image={{ include "activecheck.image.pyxis" . }} \
  --container-mounts=$(which health-checker):/usr/local/bin/health-checker,$HC_OUTPUT_DIR:$HC_OUTPUT_DIR \
  bash -c "health-checker run -e soperator -p $platform -n deviceQuery,vectorAdd,simpleMultiGPU,p2pBandwidthLatencyTest -f json-partial --tests-stdout-path /opt/soperator-outputs/health_checker_cmd_stdout")

echo "Health checker output: $HC_OUTPUT"
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
