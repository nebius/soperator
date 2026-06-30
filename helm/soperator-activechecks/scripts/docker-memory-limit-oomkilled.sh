#!/bin/bash
#SBATCH --deadline="now+1hours"
#SBATCH --time=5:00
#SBATCH --exclusive
#SBATCH --mem=0

set -euo pipefail

echo "Running docker-memory-limit-oomkilled check on $(hostname)..."

container_name="soperatorchecks-oom-$(hostname)-${SLURM_JOB_ID}-${RANDOM}"
image="{{ include "activecheck.image.docker" . }}"

cleanup() {
  docker rm -f "${container_name}" >/dev/null 2>&1 || true
}
trap cleanup EXIT

set +e
# Use --runtime=runc to bypass the NVIDIA Container Runtime default.
# This test validates Slurm memory limits, not GPU — runc works on both
# CPU and GPU nodes.
srun --mem 100m docker run \
  --runtime=runc \
  --name "${container_name}" \
  "${image}" \
  python3 -c 'buf = bytearray(101 * 1024 * 1024); print(len(buf))'
run_rc=$?
set -e

oom_killed="$(docker inspect -f '{{ "{{" }}.State.OOMKilled{{ "}}" }}' "${container_name}")"
exit_code="$(docker inspect -f '{{ "{{" }}.State.ExitCode{{ "}}" }}' "${container_name}")"

echo "docker run exit code: ${run_rc}"
echo "container exit code: ${exit_code}"
echo "container OOMKilled: ${oom_killed}"

if [[ "${oom_killed}" != "true" ]]; then
  echo "Expected container state to be OOMKilled."
  exit 1
fi

if [[ "${run_rc}" -eq 0 ]]; then
  echo "Expected docker run to fail after hitting the Slurm memory limit."
  exit 1
fi
