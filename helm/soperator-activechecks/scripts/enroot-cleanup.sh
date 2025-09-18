#!/bin/bash
#SBATCH --deadline="now+8hours"
#SBATCH --time=1:00:00
#SBATCH --exclusive
#SBATCH --mem=0

echo "Cleaning up Enroot containers on node: $(hostname)"
srun bash -c "enroot list | grep -E '^pyxis_[0-9]+\.[^.]*$' | xargs -r -n1 -- enroot remove --force"
echo "Cleanup done."
