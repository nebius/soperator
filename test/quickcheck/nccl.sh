#!/bin/bash

#SBATCH -J nccl
#SBATCH --output=/quickcheck/outputs/nccl.out
#SBATCH --error=/quickcheck/outputs/nccl.out
#SBATCH --cpus-per-task=16
#SBATCH --mem-per-cpu=8G
#SBATCH --gres=gpu:nvidia_h100_80gb_hbm3:8

# Allocate 8 ANY GPUs
srun --cpus-per-task=16 --mem-per-cpu=8G --gres=gpu:8 \
    echo "Run NCCL test with NVLink:" && /usr/bin/all_reduce_perf -b 512M -e 8G -f 2 -g 8

# Allocate 8 H100 GPUs
srun --cpus-per-task=16 --mem-per-cpu=8G --gres=gpu:nvidia_h100_80gb_hbm3:8 \
    echo "Run NCCL test with InfiniBand:" && NCCL_P2P_DISABLE=1 NCCL_SHM_DISABLE=1 NCCL_ALGO=Ring /usr/bin/all_reduce_perf -b 512M -e 8G -f 2 -g 8
