#!/bin/bash
#SBATCH --deadline="now+4hours"
#SBATCH --time=10:00
#SBATCH --exclusive
#SBATCH --mem=0

echo "Running all_reduce_perf_nccl_in_docker check on $(hostname)..."
GPUS_PER_NODE="${SBATCH_GPUS_PER_NODE:-${SLURM_GPUS_ON_NODE:-8}}"
NCCL_ENV_PREFIX="NCCL_P2P_DISABLE=1 NCCL_SHM_DISABLE=1 NCCL_ALGO=Ring "
if nvidia-smi --query-gpu=name --format=csv,noheader | grep -q "NVIDIA GB300"; then
  echo "Detected GB300 platform, running all_reduce_perf with NCCL default transport and algorithm selection"
  NCCL_ENV_PREFIX=""
fi

mkdir -p /tmp/soperatorchecks/docker_check/a
mkdir -p /tmp/soperatorchecks/docker_check/b

srun docker run --rm \
  --gpus=all --device=/dev/infiniband \
  -v /tmp/soperatorchecks/docker_check/a:/a \
  --mount type=bind,source=/tmp/soperatorchecks/docker_check/b,target=/b \
  -e NVIDIA_DISABLE_REQUIRE=1 \
  {{ include "activecheck.image.docker" . }} \
  bash -l -c "${NCCL_ENV_PREFIX}all_reduce_perf -b 512M -e 8G -f 2 -g $GPUS_PER_NODE"
