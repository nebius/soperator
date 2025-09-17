#!/bin/bash
#SBATCH --deadline="now+8hours"
#SBATCH --time=30:00
#SBATCH --exclusive
#SBATCH --mem=0
#SBATCH --partition="hidden"

srun --container-image={{ .Values.activeCheckImage }} hostname
