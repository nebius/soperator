#!/bin/bash

set -euxo pipefail # Exit immediately if any command returns a non-zero error code

PARTITION="hidden"

echo "Cancelling currently active jobs with the same name..."
scancel --partition="$PARTITION" --name="$ACTIVE_CHECK_NAME"

echo "Finding nodes to exclude..."
JQ_EXCLUDE_BAD_NODES='
.sinfo[]
| select(
    (
      .node.state
      | map(test("^(DOWN|ERROR|DRAIN|RESERVED|INVALID_REG|NOT_RESPONDING|POWER_DOWN|POWERING_DOWN|REBOOT_ISSUED|REBOOT_REQUESTED)$"))
      | any
    )
    | not
  )
| .nodes.nodes[]
'

readarray -t NODES < <(sinfo -N --partition="$PARTITION" --responding --json | jq -r "$JQ_EXCLUDE_BAD_NODES")
NUM_NODES=${#NODES[@]}

# Decide how many nodes to use: min(NUM_NODES, ACTIVE_CHECK_MAX_NUMBER_OF_JOBS), with 0/unset meaning no limit
LIMIT="${ACTIVE_CHECK_MAX_NUMBER_OF_JOBS:-0}"
if ! [[ "$LIMIT" =~ ^[0-9]+$ ]]; then
  LIMIT=0
fi

if (( LIMIT <= 0 )); then
  ARRAY_SIZE="$NUM_NODES"
else
  if (( NUM_NODES < LIMIT )); then
    ARRAY_SIZE="$NUM_NODES"
  else
    ARRAY_SIZE="$LIMIT"
  fi
fi

if (( ARRAY_SIZE <= 0 )); then
  echo "No nodes to run on (ARRAY_SIZE=$ARRAY_SIZE). Failing."
  exit 1
fi

# Randomly select ARRAY_SIZE nodes
readarray -t SELECTED_NODES < <(printf "%s\n" "${NODES[@]}" | shuf -n "$ARRAY_SIZE")

JOB_IDS=()
for node in "${SELECTED_NODES[@]}"; do
    echo "Submitting Slurm job for node $node..."

    # Here we use env variables instead of --output and --error because they do not support %N (node name) parameter.
    OUT_PATTERN='/opt/soperator-outputs/slurm_jobs/%N.%x.%j.out'
    JOB_ID=$(
        SBATCH_OUTPUT="$OUT_PATTERN" \
        SBATCH_ERROR="$OUT_PATTERN" \
        /usr/bin/sbatch --parsable \
            --job-name="$ACTIVE_CHECK_NAME" \
            --partition="$PARTITION" \
            --no-requeue \
            --nodelist="$node" \
            --nodes=1 \
            --chdir=/opt/soperator-home/soperatorchecks \
            --uid=soperatorchecks \
            /opt/bin/sbatch.sh
    ) || { echo "sbatch submission failed for node $node, skipping..."; continue; }

    if [[ -z "$JOB_ID" ]]; then
        echo "Empty output from sbatch for node $node"
        continue
    fi

    if [[ ! "$JOB_ID" =~ ^[0-9]+$ ]]; then
      echo "Unexpected sbatch output for node $node: $JOB_ID"
      continue
    fi

    JOB_IDS+=("$JOB_ID")

    sleep 0.3 # avoid DDoS-ing the controller
done

if [[ ${#JOB_IDS[@]} -eq 0 ]]; then
    echo "No jobs were scheduled successfully. Failing."
    exit 1
fi

echo "$(IFS=,; echo "${JOB_IDS[*]}")"
