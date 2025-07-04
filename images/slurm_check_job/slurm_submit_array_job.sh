#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code

echo "Setting Extra field to all nodes..."
for node in $(sinfo -N --noheader -o "%N" | tr '\n' ' '); do
    echo "Updating node: $node"
    extra_json=$(scontrol show node "$node" | awk -F= '/Extra=/{print $2}')
    if [[ -z "$extra_json" || "$extra_json" == "none" ]]; then
        extra_json="{}"
    fi
    updated_json=$(echo "$extra_json" | jq -c --arg key "$ACTIVE_CHECK_NAME" --argjson val true '.[$key] = $val')
    scontrol update NodeName="$node" Extra="$updated_json"
done

echo "Submitting Slurm array job..."
HOSTS_NUM=$(sinfo -N --noheader -o "%N" | wc -l)
export SLURM_PROLOG="/slurm/activecheck-prolog.sh"
OUT_PATTERN='/var/spool/slurmd/soperator-outputs/%N/slurm_jobs/%x.%j.%A.out'
# Here we use env variables instead of --output and --error because they do not support %N (node name) parameter.
SLURM_OUTPUT=$(
    SBATCH_OUTPUT="$OUT_PATTERN" \
    SBATCH_ERROR="$OUT_PATTERN" \
    /usr/bin/sbatch --parsable \
        --job-name="$ACTIVE_CHECK_NAME" \
        --export=ALL,SLURM_PROLOG \
        --extra="${ACTIVE_CHECK_NAME}=true" \
        --array=0-$((HOSTS_NUM-1)) \
        --nodes=1 \
        --chdir=/opt/soperatorchecks \
        --uid=soperatorchecks \
        /opt/bin/sbatch.sh
)
if [[ -z "$SLURM_OUTPUT" ]]; then
    echo "Failed to submit Slurm job"
    exit 1
fi

echo "$SLURM_OUTPUT"
