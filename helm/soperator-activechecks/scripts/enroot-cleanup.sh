#!/bin/bash
#SBATCH --deadline="now+12hours"
#SBATCH --time=1:00:00
#SBATCH --exclusive

echo "Cleaning up Enroot containers on node: $(hostname)"
srun bash -c "enroot list | grep -E '^pyxis_[0-9]+\.[^.]*$' | xargs -r -n1 -- enroot remove --force"
echo "Cleanup done."
