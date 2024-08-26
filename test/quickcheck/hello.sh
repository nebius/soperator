#!/bin/bash

#SBATCH -J hello
#SBATCH --output=/quickcheck/outputs/hello.out
#SBATCH --error=/quickcheck/outputs/hello.out
#SBATCH --cpus-per-task=120
#SBATCH --mem-per-cpu=8G
#SBATCH --gpus=4

# Print hello
srun echo "Hello from $(hostname)"

# Allocate some resources
srun --ntasks=2 --cpus-per-task=60 --mem-per-cpu=8G --gpus=4 \
    echo "Run nvidia-smi on $(hostname)" && nvidia-smi
