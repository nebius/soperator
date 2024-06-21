#!/bin/bash

#SBATCH -J enroot
#SBATCH --output=/demo/outputs/enroot.out
#SBATCH --error=/demo/outputs/enroot.out

# Specify the image in order to run the job inside a container
srun --container-image="nvidia/cuda:12.2.2-base-ubuntu20.04" \
    echo "Run nvidia-smi from enroot container on $(hostname)" && nvidia-smi
