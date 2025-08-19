#!/bin/bash

set -euxo pipefail # Exit immediately if any command returns a non-zero error code

PARTITION="hidden"

echo "Cancelling currently active jobs with the same name..."
scancel --partition="$PARTITION" --name="$ACTIVE_CHECK_NAME"

echo "Setting Extra field to all nodes..."
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
JOB_IDS=()
for node in $(sinfo -N --partition="$PARTITION" --responding --json | jq -r "$JQ_EXCLUDE_BAD_NODES"); do
    echo "Submitting Slurm job for node $node..."
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
    )

    SBATCH_STATUS=$?
    if [[ $SBATCH_STATUS -ne 0 ]]; then
        echo "sbatch failed for node $node with exit code $SBATCH_STATUS"
        continue
    fi

    if [[ -z "$JOB_ID" ]]; then
        echo "Empty output from sbatch for node $node"
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
