#!/bin/bash
#SBATCH --deadline="now+24hours"
#SBATCH --time=01:00:00
#SBATCH --gpus-per-node=8
#SBATCH --exclusive
#SBATCH --mem=0
#SBATCH --nodes=1

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

echo "Listing available health checks for platform $platform"
health-checker list -e soperator -p $platform

HC_RUN_ID=""
OUT_TMPL="/opt/soperator-outputs/slurm_jobs/$SLURMD_NODENAME.extensive-check:@TEST@.$SLURM_JOB_ID.out"
HC_CMD_OUT_DIR="/opt/soperator-outputs/health_checker_cmd_stdout"
mkdir -p "$HC_CMD_OUT_DIR"

SRUN_CONTAINER_ARGS=(
  --container-image={{ include "activecheck.image.pyxis" . }}
  --container-mounts="$(which health-checker):/usr/local/bin/health-checker,$HC_CMD_OUT_DIR:$HC_CMD_OUT_DIR"
)

HC_RUN_COMMON_ARGS=(
  -e soperator
  -p "$platform"
  -f json-partial
  --tests-stdout-path "$HC_CMD_OUT_DIR"
  --log-level info
)

parse_hc_output() {
  local HC_OUTPUT HC_STATUS JSON_BLOCK
  local output_file=$1

  if [[ ! -s "$output_file" ]]; then
    echo "Health-checker output file '$output_file' is empty or missing."
    return 0
  fi

  HC_OUTPUT=$(<"$output_file")

  echo "Health checker output:"
  echo "$HC_OUTPUT"
  JSON_BLOCK=$(echo "$HC_OUTPUT" | awk '/^\s*{/,/^\s*}/')
  HC_STATUS=$(echo "$JSON_BLOCK" | jq -r '.status // empty')
  echo "Health checker finished with status '$HC_STATUS'"

  if [[ "$HC_STATUS" == "FAIL" ]]; then
    HC_RUN_ID=$(echo "$JSON_BLOCK" | jq -r '.meta.run_id // empty')
    return 1
  elif [[ "$HC_STATUS" == "ERROR" ]]; then
    return 0
  elif [[ "$HC_STATUS" == "PASS" ]]; then
    return 0
  else
    echo "Health checker finished with unknown status."
    return 0
  fi
}

passive_checks() {
  local NAME="passive-checks"
  export SRUN_OUTPUT="${OUT_TMPL/@TEST@/$NAME}"
  export SRUN_ERROR="$SRUN_OUTPUT"

  srun -J "$NAME" \
    sudo bash -l -c \
      "cd /tmp && \
      HC_DCGMI_DIAG_R1_DEBUGLOGFILE=/dev/null \
      HC_DCGMI_DIAG_R1_DEBUGLEVEL=NONE \
      health-checker run ${HC_RUN_COMMON_ARGS[*]} \
      -n module,nvidia_smi,nvidia_smi_nvlink,nvidia_smi_topo,dmesg,ib_link,dcgmi_diag_r1"

  parse_hc_output "$SRUN_OUTPUT"
}

all_reduce_with_ib() {
  local NAME="all-reduce-perf-nccl-with-ib"
  export SRUN_OUTPUT="${OUT_TMPL/@TEST@/$NAME}"
  export SRUN_ERROR="$SRUN_OUTPUT"

  srun -J "$NAME" \
    sudo bash -l -c \
      "health-checker run ${HC_RUN_COMMON_ARGS[*]} \
      -n all_reduce_with_ib"

  parse_hc_output "$SRUN_OUTPUT"
}

all_reduce_without_ib() {
  local NAME="all-reduce-perf-nccl-without-ib"
  export SRUN_OUTPUT="${OUT_TMPL/@TEST@/$NAME}"
  export SRUN_ERROR="$SRUN_OUTPUT"

  srun -J "$NAME" \
    sudo bash -l -c \
      "health-checker run ${HC_RUN_COMMON_ARGS[*]} \
      -n all_reduce_without_ib"

  parse_hc_output "$SRUN_OUTPUT"
}

cuda_samples() {
  local NAME="cuda-samples"
  export SRUN_OUTPUT="${OUT_TMPL/@TEST@/$NAME}"
  export SRUN_ERROR="$SRUN_OUTPUT"

  srun -J "$NAME" \
    "${SRUN_CONTAINER_ARGS[@]}" \
    bash -l -c \
      "health-checker run ${HC_RUN_COMMON_ARGS[*]} \
      -n deviceQuery,vectorAdd,simpleMultiGPU,p2pBandwidthLatencyTest"

  parse_hc_output "$SRUN_OUTPUT"
}

dcgmi_diag_r2() {
  local NAME="dcgmi-diag-r2"
  export SRUN_OUTPUT="${OUT_TMPL/@TEST@/$NAME}"
  export SRUN_ERROR="$SRUN_OUTPUT"

  srun -J "$NAME" \
    sudo bash -l -c \
      "health-checker run ${HC_RUN_COMMON_ARGS[*]} \
      -n dcgmi_diag_r2"

  parse_hc_output "$SRUN_OUTPUT"
}

gpu_fryer() {
  local NAME="gpu-fryer"
  export SRUN_OUTPUT="${OUT_TMPL/@TEST@/$NAME}"
  export SRUN_ERROR="$SRUN_OUTPUT"

  srun -J "$NAME" \
    "${SRUN_CONTAINER_ARGS[@]}" \
    bash -l -c \
      "HC_GPU_FRYER_DURATION=300 \
      health-checker run ${HC_RUN_COMMON_ARGS[*]} \
      -n gpu_fryer"

  parse_hc_output "$SRUN_OUTPUT"
}

ib_gpu_perf() {
  local NAME="ib-gpu-perf"
  export SRUN_OUTPUT="${OUT_TMPL/@TEST@/$NAME}"
  export SRUN_ERROR="$SRUN_OUTPUT"

  srun -J "$NAME" \
    "${SRUN_CONTAINER_ARGS[@]}" \
    bash -l -c \
      "health-checker run ${HC_RUN_COMMON_ARGS[*]} \
      -n ^ib_write_bw_gpu.*$,^ib_send_lat_gpu.*$,^ib_read_lat_gpu.*$"

  parse_hc_output "$SRUN_OUTPUT"
}

mem_perf() {
  local NAME="mem-perf"
  export SRUN_OUTPUT="${OUT_TMPL/@TEST@/$NAME}"
  export SRUN_ERROR="$SRUN_OUTPUT"

  srun -J "$NAME" \
    "${SRUN_CONTAINER_ARGS[@]}" \
    bash -l -c \
      "health-checker run ${HC_RUN_COMMON_ARGS[*]} \
      -n mem_bw,mem_lat"

  parse_hc_output "$SRUN_OUTPUT"
}

health_checker_runs=(
  passive_checks
  all_reduce_with_ib
  all_reduce_without_ib
  cuda_samples
  dcgmi_diag_r2
  gpu_fryer
#  ib_gpu_perf
  mem_perf
)
for hc_run in "${health_checker_runs[@]}"
do
  echo "Start health-checker run '$hc_run' on $(hostname)..."
  HC_RUN_ID=""
  $hc_run
  RUN_EXIT_CODE=$?

  if [[ $RUN_EXIT_CODE -eq 1 ]]; then
    NODE_NAME=$(hostname)
    echo "Setting comment on node $NODE_NAME"
    COMPUTE_INSTANCE_ID=$(scontrol show node "$NODE_NAME" --json | jq -r '.nodes[0].instance_id')
    
    # Build a JSON object with common values and merge it with SLURM_EXTRA_COMMENT_JSON
    COMMENT=$(jq -cn \
          --arg run "$HC_RUN_ID" \
          --arg inst "$COMPUTE_INSTANCE_ID" \
          --arg extra "${SLURM_EXTRA_COMMENT_JSON:-\{\}}" \
          '{
            health_checker_run_id: $run,
            compute_instance_id: $inst
          } + ($extra | fromjson? // {})')
    
    sudo scontrol update NodeName="$NODE_NAME" Comment="$COMMENT"
    exit 1
  fi
done
