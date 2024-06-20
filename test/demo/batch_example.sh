#!/bin/bash

#SBATCH -J example
#SBATCH --output=/home/bob/outputs/example.out
#SBATCH --nodes 2
#SBATCH --gres=gpu:nvidia_h100_80gb_hbm3:8
#SBATCH --ntasks=2
#SBATCH --cpus-per-task=4
#SBATCH --mem=8G

srun -N 2 echo "Hello from $(hostname)"

srun --ntasks=2 --cpus-per-task=4 --gpus=8 \
    echo "### Run nvidia-smi on $(hostname)" && nvidia-smi
