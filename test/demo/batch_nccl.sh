#!/bin/bash

#SBATCH -J nccl
#SBATCH --output=/home/bob/outputs/nccl.out
#SBATCH --gres=gpu:nvidia_h100_80gb_hbm3:8

srun echo '### Run NCCL test with NVLink' && \
    /usr/bin/all_reduce_perf -b 512M -e 8G -f 2 -g 8

srun echo '### Run NCCL test with InfiniBand' && \
    NCCL_P2P_DISABLE=1 NCCL_SHM_DISABLE=1 NCCL_ALGO=Ring /usr/bin/all_reduce_perf -b 512M -e 8G -f 2 -g 8
