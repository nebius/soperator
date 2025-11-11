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

LAST_RUN_ID=""
LAST_FAIL_TEST=""
LAST_FAIL_ERROR=""

_run_and_parse_hc() {
  local HC_OUTPUT HC_STATUS JSON_BLOCK FIRST_FAIL
  HC_OUTPUT=$("$@")

  echo "Health checker output: $HC_OUTPUT"
  JSON_BLOCK=$(echo "$HC_OUTPUT" | awk '/^\s*{/,/^\s*}/')
  HC_STATUS=$(echo "$JSON_BLOCK" | jq -r '.status')
  echo "Health checker status: $HC_STATUS"

  if [[ "$HC_STATUS" == "FAIL" ]]; then
    LAST_RUN_ID=$(echo "$JSON_BLOCK" | jq -r '.meta.run_id // "undefined"')
    FIRST_FAIL=$(echo "$JSON_BLOCK" | jq -r '
      [ .tests[] as $t
        | $t.checks[]
        | select((.state.status // .status) == "FAIL")
        | {test: $t.name, error: (.state.error // .error // "undefined")}
      ][0]
      | if . then "\(.test)|\(.error)" else "" end
    ')
    if [[ -n "$FIRST_FAIL" ]]; then
      IFS='|' read -r LAST_FAIL_TEST LAST_FAIL_ERROR <<< "$FIRST_FAIL"
    else
      LAST_FAIL_TEST="undefined"
      LAST_FAIL_ERROR="undefined"
    fi

    echo "Health-checker reported status=FAIL."
    return 1
  elif [[ "$HC_STATUS" == "ERROR" ]]; then
    echo "Health-checker reported status=ERROR."
    return 0
  else
    echo "Health-checker passed or returned non-ERROR status."
    return 0
  fi
}

passive_checks() {
  _run_and_parse_hc srun --cpu-bind=verbose,cores bash -c \
    "cd /tmp && \
    HC_DCGMI_DIAG_R1_DEBUGLOGFILE=/dev/null HC_DCGMI_DIAG_R1_DEBUGLEVEL=NONE \
    health-checker run -e soperator -p $platform \
    -n module,nvidia_smi,nvidia_smi_nvlink,nvidia_smi_topo,dmesg,ib_link,dcgmi_diag_r1 \
    -f json-partial --tests-stdout-path /opt/soperator-outputs/health_checker_cmd_stdout \
    --log-level info"
}

all_reduce_in_docker() {
  mkdir -p /tmp/soperatorchecks/a
  mkdir -p /tmp/soperatorchecks/b

  srun --gpus=8 docker run --rm \
    --gpus=all --device=/dev/infiniband \
    -v /tmp/soperatorchecks/a:/a \
    --mount type=bind,source=/tmp/soperatorchecks/b,target=/b \
    {{ include "activecheck.image.docker" . }} \
    bash -c "NCCL_P2P_DISABLE=1 NCCL_SHM_DISABLE=1 NCCL_ALGO=Ring all_reduce_perf -b 512M -e 8G -f 2 -g 8"
  local rc=$?

  if [[ $rc -ne 0 ]]; then
    LAST_RUN_ID="undefined"
    LAST_FAIL_TEST="all_reduce_in_docker"
    LAST_FAIL_ERROR="all_reduce_perf exited with non-zero status"
    return 1
  fi

  return 0
}

all_reduce_with_ib() {
  _run_and_parse_hc srun --cpu-bind=verbose,cores bash -c "health-checker run -e soperator -p $platform -n all_reduce_with_ib -f json-partial --tests-stdout-path /opt/soperator-outputs/health_checker_cmd_stdout --log-level info"
}

all_reduce_without_ib() {
  _run_and_parse_hc srun --cpu-bind=verbose,cores bash -c "health-checker run -e soperator -p $platform -n all_reduce_without_ib -f json-partial --tests-stdout-path /opt/soperator-outputs/health_checker_cmd_stdout --log-level info"
}

cuda_samples() {
  _run_and_parse_hc srun --cpu-bind=verbose --container-image={{ include "activecheck.image.pyxis" . }} \
    --container-mounts=$(which health-checker):/usr/local/bin/health-checker \
    bash -c "health-checker run -e soperator -p $platform -n deviceQuery,vectorAdd,simpleMultiGPU,p2pBandwidthLatencyTest -f json-partial --tests-stdout-path /opt/soperator-outputs/health_checker_cmd_stdout"
}

dcgmi_diag_r2() {
  _run_and_parse_hc srun --cpu-bind=verbose,cores bash -c "health-checker run -e soperator -p $platform -n dcgmi_diag_r2 -f json-partial --tests-stdout-path /opt/soperator-outputs/health_checker_cmd_stdout"
}

gpu_fryer() {
  _run_and_parse_hc srun --cpu-bind=verbose --container-image={{ include "activecheck.image.pyxis" . }} \
    --container-mounts=$(which health-checker):/usr/local/bin/health-checker \
    bash -c "HC_GPU_FRYER_DURATION=300 health-checker run -e soperator -p $platform -n gpu_fryer -f json-partial --tests-stdout-path /opt/soperator-outputs/health_checker_cmd_stdout"
}

ib_gpu_perf() {
  _run_and_parse_hc srun --container-image={{ include "activecheck.image.pyxis" . }} \
    --container-mounts=$(which health-checker):/usr/local/bin/health-checker --cpu-bind=verbose,cores \
    bash -c "health-checker run -e soperator -p $platform -n ^ib_write_bw_gpu.*$,^ib_send_lat_gpu.*$,^ib_read_lat_gpu.*$ -f json-partial --tests-stdout-path /opt/soperator-outputs/health_checker_cmd_stdout"
}

mem_perf() {
  _run_and_parse_hc srun --container-image={{ include "activecheck.image.pyxis" . }} \
    --container-mounts=$(which health-checker):/usr/local/bin/health-checker --cpu-bind=verbose,cores \
    bash -c "health-checker run -e soperator -p $platform -n mem_bw,mem_lat -f json-partial --tests-stdout-path /opt/soperator-outputs/health_checker_cmd_stdout"
}

funcs_to_test=(
  passive_checks
  all_reduce_in_docker
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
  echo "Running $test on $(hostname)..."
  $test
  TEST_EXIT_CODE=$?

  if [[ $TEST_EXIT_CODE -ne 0 ]]; then
    COMMENT="Run ID: ${LAST_RUN_ID}, FirstFailedCheck: ${LAST_FAIL_TEST}, Error: ${LAST_FAIL_ERROR}"
    NODE_NAME=$(hostname)
    echo "Setting node comment: $COMMENT"
    sudo scontrol update NodeName=$NODE_NAME Comment="$COMMENT"

    echo "$test reported failure (exit code $TEST_EXIT_CODE)."
    exit 1
  else
    echo "$test passed."
  fi
done
