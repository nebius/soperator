#!/bin/bash
#SBATCH --time=01:00:00
#SBATCH --gpus-per-node=8

echo "Upgrading nc-health-checker to the version 1.0.0-147.250819"
# We could use `retry -d 2 -t 10 --` here but it's not currently installed in jail.
ssh -i /mnt/jail/opt/soperator-home/soperatorchecks/.ssh/soperatorchecks_id_ecdsa \
    -o StrictHostKeyChecking=no \
    soperatorchecks@login-0.soperator-login-headless-svc.soperator.svc.cluster.local \
    'flock /var/lock/apt.lock bash -c "sudo apt update && sudo apt install --only-upgrade nc-health-checker=1.0.0-147.250819 && apt-cache policy nc-health-checker"'

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

echo "Listing available health checks for platform $platform"
health-checker list -e soperator -p $platform

CONTAINER_IMAGE="cr.eu-north1.nebius.cloud#soperator/active_checks:12.9.0-ubuntu24.04-nccl_tests2.16.4-b8189f7"

all_reduce_without_ib() {
  health-checker run -e soperator -p $platform -n all_reduce_without_ib --report-format json-pretty
}

all_reduce_with_ib() {
  health-checker run -e soperator -p $platform -n all_reduce_with_ib --report-format json-pretty
}

mem_bw() {
  srun --container-image="$CONTAINER_IMAGE" \
  --container-mounts=$(which health-checker):/usr/local/bin/health-checker \
  --cpu-bind=verbose,cores \
  bash -c "health-checker run -e soperator -p $platform -n mem_bw --report-format json-pretty"
}

mem_lat() {
  srun --container-image="$CONTAINER_IMAGE" \
       --container-mounts=$(which health-checker):/usr/local/bin/health-checker \
       --cpu-bind=verbose,cores \
       bash -c "health-checker run -e soperator -p $platform -n mem_lat --report-format json-pretty"
}

cuda_samples() {
  srun --container-image="$CONTAINER_IMAGE" \
        --container-mounts=$(which health-checker):/usr/local/bin/health-checker \
        --cpu-bind=verbose \
        bash -c "health-checker run -e soperator -p $platform -n deviceQuery,vectorAdd,simpleMultiGPU,p2pBandwidthLatencyTest --report-format json-pretty"
}

dcgmi_diag_r2() {
  health-checker run -e soperator -p $platform -n dcgmi_diag_r2 --report-format json-pretty
}

gpu_fryer() {
  srun --container-image="$CONTAINER_IMAGE" \
  --container-mounts=$(which health-checker):/usr/local/bin/health-checker \
  --cpu-bind=verbose \
  bash -c "HC_GPU_FRYER_DURATION=300 health-checker run -e soperator -p $platform -n gpu_fryer --report-format json-pretty"
}

funcs_to_test=(
  all_reduce_without_ib
  all_reduce_with_ib
  # mem_bw: gives an error on B200: Error: unable to bind thread to core 159 with hwid 159
  #         Let's keep it out for now.
  mem_lat
  cuda_samples
  dcgmi_diag_r2
  gpu_fryer
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
