#!/bin/bash

set -euxo pipefail

echo "Cleaning old Soperator outputs"

find /mnt/jail/opt/soperator-outputs -type f -mmin +60 -print -delete

clean_sncclprecon_dump_dirs() {
  if [[ "$SNCCLPRECON_DUMP_DIR" != /* || "$SNCCLPRECON_DUMP_DIR" == "/" ]]; then
    echo "Skipping NCCL Inspector Profiling dump cleanup: SNCCLPRECON_DUMP_DIR must be an absolute non-root path"
    return
  fi

  local dump_dir="/mnt/jail${SNCCLPRECON_DUMP_DIR}"

  if [[ ! -d "$dump_dir" ]]; then
    echo "Skipping NCCL Inspector Profiling dump cleanup: ${dump_dir} does not exist"
    return
  fi

  echo "Cleaning empty NCCL Inspector Profiling dump job directories in ${dump_dir}"

  find "$dump_dir" -mindepth 1 -maxdepth 1 -type d -print0 | while IFS= read -r -d '' job_dir; do
    # If there are log files left
    if [[ -n "$(find "$job_dir" -type f -print -quit)" ]]; then
      continue
    fi

    # If there are no step directories for the job
    if [[ -z "$(find "$job_dir" -mindepth 1 -maxdepth 1 -type d -print -quit)" ]]; then
      # If the job directory itself is changed recently, it may be a new job without steps created yet, so skip removing it
      if [[ -n "$(find "$job_dir" -maxdepth 0 -type d -mmin -180 -print -quit)" ]]; then
        continue
      fi

      echo "Removing stale empty NCCL Inspector Profiling dump job directory without steps: ${job_dir}"
      rm -rf -- "$job_dir"
      continue
    fi

    # If there are step directories changed recently
    if [[ -n "$(find "$job_dir" -mindepth 1 -maxdepth 1 -type d -mmin -180 -print -quit)" ]]; then
      continue
    fi

    echo "Removing empty NCCL Inspector Profiling dump job directory: ${job_dir}"
    rm -rf -- "$job_dir"
  done
}

if [[ -n "${SNCCLPRECON_DUMP_DIR:-}" ]]; then
  clean_sncclprecon_dump_dirs
fi
