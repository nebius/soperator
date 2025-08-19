#!/bin/bash
#SBATCH --time=01:00:00
#SBATCH --gpus-per-node=8

apt update
apt install --only-upgrade nc-health-checker
apt-cache policy nc-health-checker

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


# (1) I don't get why we use srun here. We are already on a worker node!
# srun --cpu-bind=verbose,cores --gpus-per-node=8 bash -c "health-checker run -e soperator -p $platform -n all_reduce_without_ib --report-format json-pretty"

# (2) Do we need --cpu-bind=verbose,cores for the following tests? and why?
# - all_reduce_without_ib
# - all_reduce_with_ib
# - mem_bw
# - mem_lat

all_reduce_without_ib() {
  health-checker run -e soperator -p $platform -n all_reduce_without_ib --report-format json-pretty
}

all_reduce_with_ib() {
  health-checker run -e soperator -p $platform -n all_reduce_with_ib --report-format json-pretty
}

mem_bw() {
  health-checker run -e soperator -p $platform -n mem_bw --report-format json-pretty
}

mem_lat() {
  health-checker run -e soperator -p $platform -n mem_lat --report-format json-pretty
}

gpu_fryer() {
  HC_GPU_FRYER_DURATION=120 health-checker run -e soperator -p $platform -n gpu_fryer --report-format json-pretty
}

funcs_to_test=(all_reduce_without_ib all_reduce_with_ib mem_bw mem_lat gpu_fryer)
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
  if [[ "$HC_STATUS" == "ERROR" && $HC_EXIT_CODE -eq 1 ]]; then
    echo "Health-checker reported status=ERROR and exited with non-zero status."
    exit 1 # Fail fast 
  else
    echo "Health-checker passed or returned non-ERROR status."
  fi
done
