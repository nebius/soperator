#!/bin/bash

set -euxo pipefail

echo "[$(date)] Delete tmpfs directories for all jobs that don't run on this node"

TMPFS_DIR="/mnt/jail/mnt/memory"
AGE_THRESHOLD_SEC=60

# Validate tmpfs directory
if [ ! -d "${TMPFS_DIR}" ]; then
    echo "${TMPFS_DIR} doesn't exist"
    exit 0
fi
if ! mountpoint -q "${TMPFS_DIR}"; then
    echo "${TMPFS_DIR} is not a mountpoint"
    exit 0
fi

shopt -s nullglob

echo "Get the list of jobs with existing tmpfs dirs"
declare -a dir_job_ids=()
for job_tmpfs_dir in "$TMPFS_DIR"/job_*; do
    [[ -d "$job_tmpfs_dir" ]] || continue
    base="${job_tmpfs_dir##*/}"
    job_id="${base#job_}"
    if [[ "$job_id" =~ ^[0-9]+$ ]]; then
        dir_job_ids+=("$job_id")
    else
        echo "Skipping unexpected directory ($job_tmpfs_dir)"
    fi
done

if ((${#dir_job_ids[@]} == 0)); then
    echo "No job tmpfs directories present"
    exit 0
fi

# Fetch scontrol JSON safely; if we can't, skip cleanup (don't risk deleting good dirs)
if ! running_jobs_json="$(scontrol listjobs --json 2>/dev/null)"; then
    echo "Failed to fetch jobs from scontrol, exiting"
    exit 0
fi
if ! jq -e . >/dev/null 2>&1 <<<"$running_jobs_json"; then
    echo "Got invalid JSON from scontrol listjobs, exiting"
    exit 0
fi
mapfile -t running_jobs < <(jq -r '.jobs // [] | .[].job_id' <<<"$running_jobs_json")
declare -A running_map=()
for id in "${running_jobs[@]}"; do
    [[ -n "$id" ]] && running_map["$id"]=1
done

now_epoch="$(date +%s)"

for id in "${dir_job_ids[@]}"; do
    job_tmpfs_dir="$TMPFS_DIR/job_$id"

    # Skip if the directory doesn't exist
    [ -d "$job_tmpfs_dir" ] || continue

    # If this job is running, keep its directory and continue
    if [[ -n "${running_map[$id]+x}" ]]; then
        echo "Keeping running job tmpfs directory ($job_tmpfs_dir)"
        continue
    fi

    # Age check to avoid fresh races
    if stat_mtime=$(stat -c %Y "$job_tmpfs_dir" 2>/dev/null); then
        age=$(( now_epoch - stat_mtime ))
        if (( age < AGE_THRESHOLD_SEC )); then
            echo "Skipping young dir with age ${age}s < ${AGE_THRESHOLD_SEC}s ($job_tmpfs_dir)"
            continue
        fi
    fi

    echo "Deleting non-running job tmpfs directory ($job_tmpfs_dir)"
    rm -rf --one-file-system -- "$job_tmpfs_dir"
done

shopt -u nullglob

exit 0
