#!/bin/bash
#SBATCH --deadline="now+24hours"
#SBATCH --time=01:00:00
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
else
  echo "Unsupported platform"
  exit 0
fi
echo "Platform found: $platform"

echo "Listing available health checks for platform $platform"
health-checker list -e soperator -p $platform

all_reduce_without_ib() {
  health-checker run -e soperator -p $platform -n all_reduce_without_ib --report-format json-pretty
}

all_reduce_with_ib() {
  health-checker run -e soperator -p $platform -n all_reduce_with_ib --report-format json-pretty
}

mem_bw() {
  srun --container-image="$ACTIVE_CHECKS_IMAGE" \
  --container-mounts=$(which health-checker):/usr/local/bin/health-checker \
  --cpu-bind=verbose,cores \
  bash -c "health-checker run -e soperator -p $platform -n mem_bw --report-format json-pretty"
}

mem_lat() {
  srun --container-image="$ACTIVE_CHECKS_IMAGE" \
       --container-mounts=$(which health-checker):/usr/local/bin/health-checker \
       --cpu-bind=verbose,cores \
       bash -c "health-checker run -e soperator -p $platform -n mem_lat --report-format json-pretty"
}



dcgmi_diag_r2() {
  health-checker run -e soperator -p $platform -n dcgmi_diag_r2 --report-format json-pretty
}

gpu_fryer() {
  srun --container-image="$ACTIVE_CHECKS_IMAGE" \
  --container-mounts=$(which health-checker):/usr/local/bin/health-checker \
  --cpu-bind=verbose \
  bash -c "HC_GPU_FRYER_DURATION=300 health-checker run -e soperator -p $platform -n gpu_fryer --report-format json-pretty"
}

cuda_samples() {
  srun --container-image="$ACTIVE_CHECKS_IMAGE" \
        --container-mounts=$(which health-checker):/usr/local/bin/health-checker \
        --cpu-bind=verbose \
        bash -c "health-checker run -e soperator -p $platform -n deviceQuery,vectorAdd,simpleMultiGPU,p2pBandwidthLatencyTest --report-format json-pretty"
}

funcs_to_test=(
  all_reduce_without_ib
  all_reduce_with_ib
  mem_bw
  mem_lat
  dcgmi_diag_r2
  gpu_fryer
  cuda_samples
)
for test in "${funcs_to_test[@]}"
do
  echo "Running $test"
  echo "on $(hostname)..."
  HC_OUTPUT=$($test)
  HC_EXIT_CODE=$?

  echo "Health checker output: $HC_OUTPUT"
  echo "Health checker exit code: $HC_EXIT_CODE"
  HC_STATUS=$(echo "$HC_OUTPUT" | awk '/^\s*{/,/^\s*}/' | jq -r '.status')

  echo "Health checker status: $HC_STATUS"
  if [[ "$HC_STATUS" == "ERROR" || "$HC_STATUS" == "FAIL" || $HC_EXIT_CODE -eq 1 ]]; then
    echo "Health-checker reported status=ERROR and exited with non-zero status."
    exit 1 # Fail fast
  else
    echo "Health-checker passed or returned non-ERROR status."
  fi
done
