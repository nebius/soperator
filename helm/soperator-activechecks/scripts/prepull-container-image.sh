#!/bin/bash
#SBATCH --deadline="now+6hours"
#SBATCH --time=15:00

srun --container-image={{ .Values.activeCheckImage }} hostname
