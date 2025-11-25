#!/bin/bash
#SBATCH --deadline="now+4hours"
#SBATCH --time=10:00
#SBATCH --gpus-per-node=8
#SBATCH --exclusive
#SBATCH --mem=0


echo "Running all_reduce_perf_nccl_in_docker check on $(hostname)..."

mkdir -p /tmp/soperatorchecks/a
mkdir -p /tmp/soperatorchecks/b

srun --gpus=8 docker run --rm \
  --gpus=all --device=/dev/infiniband \
  -v /tmp/soperatorchecks/a:/a \
  --mount type=bind,source=/tmp/soperatorchecks/b,target=/b \
  {{ include "activecheck.image.docker" . }} \
  bash -l -c "NCCL_P2P_DISABLE=1 NCCL_SHM_DISABLE=1 NCCL_ALGO=Ring all_reduce_perf -b 512M -e 8G -f 2 -g 8"

