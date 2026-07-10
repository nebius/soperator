#!/bin/bash
#SBATCH --deadline="now+8hours"
#SBATCH --time=30:00
#SBATCH --gpus-per-node=8
#SBATCH --exclusive
#SBATCH --mem=0

rc=0
srun --container-image={{ include "activecheck.image.pyxis" . }} hostname || rc=$?

if [ "${rc}" -ne 0 ]; then
    # NVML diagnostics: the enroot nvidia hook fails when the ldconfig soname symlinks are missing in the jail.
    # No libnvidia-ml.so.1 below -> complement_jail.sh ldconfig never created it (see the slurmd pod logs of the flock winner);
    # 0-size versioned libs -> bind mounts are not visible in this node's view of the jail.
    # timeout: diagnostics must never convert a fast FAILED into a 30-min job TIMEOUT if the jail FS ever blocks reads.
    echo "[nvml] === jail GPU lib state on $(hostname) after failure ==="
    timeout 30 ls -la "/usr/lib/$(uname -m)-linux-gnu/" | awk '/libnvidia-ml|libcuda\.so/ {print "[nvml]   " $0; n++} END {if (!n) print "[nvml]   no libnvidia-ml/libcuda entries"}'
    echo "[nvml] nvidia entries in ld.so.cache: $(timeout 30 ldconfig -p | grep -c nvidia)"
fi
exit "${rc}"
