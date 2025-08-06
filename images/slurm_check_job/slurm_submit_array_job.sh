#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code

PARTITION="background"

echo "Cancelling currently active jobs with the same name..."
scancel --partition="$PARTITION" --name="$ACTIVE_CHECK_NAME"

echo "Setting Extra field to all nodes..."
JQ_EXCLUDE_BAD_NODES='
.sinfo[]
| select(
    (
      .node.state
      | map(test("^(DOWN|ERROR|DRAIN|INVALID_REG|NOT_RESPONDING|POWER_DOWN|POWERING_DOWN|REBOOT_ISSUED|REBOOT_REQUESTED)$"))
      | any
    )
    | not
  )
| .nodes.nodes[]
'
NUM_NODES=0
for node in $(sinfo -N --partition="$PARTITION" --responding --json | jq -r "$JQ_EXCLUDE_BAD_NODES"); do
    echo "Updating node: $node"
    extra_json=$(scontrol show node "$node" | awk -F= '/Extra=/{print $2}')
    if [[ -z "$extra_json" || "$extra_json" == "none" ]]; then
        extra_json="{}"
    fi
    updated_json=$(echo "$extra_json" | jq -c --arg key "$ACTIVE_CHECK_NAME" --argjson val true '.[$key] = $val')
    scontrol update NodeName="$node" Extra="$updated_json"
    NUM_NODES=$(( NUM_NODES + 1 ))
done

echo "Submitting Slurm array job..."
export SLURM_PROLOG="/etc/slurm/activecheck-prolog.sh"
OUT_PATTERN='/opt/soperator-outputs/slurm_jobs/%N.%x.%j.%A.out'
# Here we use env variables instead of --output and --error because they do not support %N (node name) parameter.
SLURM_OUTPUT=$(
    SBATCH_OUTPUT="$OUT_PATTERN" \
    SBATCH_ERROR="$OUT_PATTERN" \
    /usr/bin/sbatch --parsable \
        --job-name="$ACTIVE_CHECK_NAME" \
        --partition="$PARTITION" \
        --no-requeue \
        --export="ALL,SLURM_PROLOG" \
        --extra="${ACTIVE_CHECK_NAME}=true" \
        --array=0-$((NUM_NODES-1)) \
        --nodes=1 \
        --chdir=/opt/soperator-home/soperatorchecks \
        --uid=soperatorchecks \
        /opt/bin/sbatch.sh
)

SBATCH_STATUS=$?
if [[ $SBATCH_STATUS -ne 0 ]]; then
    echo "sbatch failed with exit code $SBATCH_STATUS"
    exit 1
fi

if [[ -z "$SLURM_OUTPUT" ]]; then
    echo "Empty output from sbatch"
    exit 1
fi

echo "$SLURM_OUTPUT"
