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

all_reduce_with_ib() {
  srun --cpu-bind=verbose,cores bash -c "health-checker run -e soperator -p $platform -n all_reduce_with_ib -f json-partial --tests-stdout-path /opt/soperator-outputs/health_checker_cmd_stdout --log-level info"
}

all_reduce_without_ib() {
  srun --cpu-bind=verbose,cores bash -c "health-checker run -e soperator -p $platform -n all_reduce_without_ib -f json-partial --tests-stdout-path /opt/soperator-outputs/health_checker_cmd_stdout --log-level info"
}

cuda_samples() {
  srun --cpu-bind=verbose --container-image={{ include "activecheck.image.pyxis" . }} \
    --container-mounts=$(which health-checker):/usr/local/bin/health-checker \
    bash -c "health-checker run -e soperator -p $platform -n deviceQuery,vectorAdd,simpleMultiGPU,p2pBandwidthLatencyTest -f json-partial --tests-stdout-path /opt/soperator-outputs/health_checker_cmd_stdout"
}

dcgmi_diag_r2() {
  srun --cpu-bind=verbose,cores bash -c "health-checker run -e soperator -p $platform -n dcgmi_diag_r2 -f json-partial --tests-stdout-path /opt/soperator-outputs/health_checker_cmd_stdout"
}

gpu_fryer() {
  srun --cpu-bind=verbose --container-image={{ include "activecheck.image.pyxis" . }} \
    --container-mounts=$(which health-checker):/usr/local/bin/health-checker \
    bash -c "HC_GPU_FRYER_DURATION=300 health-checker run -e soperator -p $platform -n gpu_fryer -f json-partial --tests-stdout-path /opt/soperator-outputs/health_checker_cmd_stdout"
}

ib_gpu_perf() {
  srun --container-image={{ include "activecheck.image.pyxis" . }} \
    --container-mounts=$(which health-checker):/usr/local/bin/health-checker --cpu-bind=verbose,cores \
    bash -c "health-checker run -e soperator -p $platform -n ^ib_write_bw_gpu.*$,^ib_send_lat_gpu.*$,^ib_read_lat_gpu.*$ -f json-partial --tests-stdout-path /opt/soperator-outputs/health_checker_cmd_stdout"
}

mem_perf() {
  srun --container-image={{ include "activecheck.image.pyxis" . }} \
    --container-mounts=$(which health-checker):/usr/local/bin/health-checker --cpu-bind=verbose,cores \
    bash -c "health-checker run -e soperator -p $platform -n mem_bw,mem_lat -f json-partial --tests-stdout-path /opt/soperator-outputs/health_checker_cmd_stdout"
}

# TODO: add all_reduce_in_docker and passive checks
funcs_to_test=(
  all_reduce_with_ib
  all_reduce_without_ib
  cuda_samples
  dcgmi_diag_r2
  gpu_fryer
  ib_gpu_perf
  mem_perf
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
