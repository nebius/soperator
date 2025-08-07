#!/bin/bash
#SBATCH --deadline="now+12hours"
#SBATCH --nodes=1
#SBATCH --ntasks=2
#SBATCH --cpus-per-task=2
#SBATCH --time=00:10:00

set -euo pipefail

PORT=18001

echo "===== CPU-to-CPU RDMA Test Starting ====="
echo "[INFO] Running ib_write_bw server/client across 2 tasks on same node using port $PORT"
echo "[INFO] SLURM Node: $(hostname)"
echo "[INFO] SLURM Job ID: $SLURM_JOB_ID"
echo "-----------------------------------------"

srun --ntasks=2 --exclusive --cpu-bind=cores bash -c "
  set -e
  if [[ \$SLURM_PROCID -eq 0 ]]; then
    echo \"[SERVER] Starting ib_write_bw --port=$PORT\"
    ib_write_bw --port=$PORT
  else
    sleep 2
    echo \"[CLIENT] Starting ib_write_bw localhost --port=$PORT\"
    ib_write_bw localhost --port=$PORT
  fi
" || {
  echo "[ERROR] RDMA test failed — ib_write_bw exited with non-zero status"
  exit 1
}

echo "===== RDMA Test Complete: SUCCESS ====="
