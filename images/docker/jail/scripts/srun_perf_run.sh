#!/bin/bash

while getopts ":b:e:f:d:" opt; do
  case ${opt} in
    b )
      min_bytes=$OPTARG
      ;;
    e )
      max_bytes=$OPTARG
      ;;
    f )
      step_factor=$OPTARG
      ;;
    d )
      drain_state=$OPTARG
      ;;
    \? )
      echo "Invalid option: $OPTARG" 1>&2
      exit 1
      ;;
    : )
      echo "Invalid option: $OPTARG requires an argument" 1>&2
      exit 1
      ;;
  esac
done
shift $((OPTIND -1))

if [ -z "$min_bytes" ] || [ -z "$max_bytes" ] || [ -z "$step_factor" ] || [ -z "$drain_state" ]; then
    echo "Usage: $0 -b <min_bytes> -e <max_bytes> -f <step_factor> -d <drain_state>" >&2
    exit 1
fi

# TODO: MSP-2184
#export NCCL_P2P_DISABLE=1
#export NCCL_SHM_DISABLE=1
#export NCCL_ALGO=Ring

perf_output=$(/usr/bin/all_reduce_perf -b "$min_bytes" -e "$max_bytes" -f "$step_factor" -g "$SLURM_GPUS")
echo "Performance output: $perf_output"

avg_bandwidth=$(echo "$perf_output" | awk '/Avg bus bandwidth/ {print $NF}')
if [ -z "$avg_bandwidth" ]; then
  echo "No AVG bandwidth output, test in trouble"
  exit 1
fi

current_node=$(hostname)
echo "Current node: $current_node"

if [ "$(echo "$avg_bandwidth == 0" | bc)" -eq 1 ]; then
  echo "Avg bus bandwidth = $avg_bandwidth"
  if [ "$drain_state" = "true" ]; then
    reason="GPUPerfProblem avg bus bandwidth = $avg_bandwidth"
    scontrol update NodeName="$current_node" State=drain Reason="$reason"
    echo "$(hostname) node drained with reason: $reason"
  fi
  exit 1
else
  echo "Avg bus bandwidth > 0: $avg_bandwidth"
  echo "Performance test completed at $(date)"
fi
