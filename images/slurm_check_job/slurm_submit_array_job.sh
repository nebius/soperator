#!/bin/bash

set -euxo pipefail # Exit immediately if any command returns a non-zero error code

PARTITION="background"

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

echo "Setting Extra field to all nodes..."
chroot --userspec=soperatorchecks:soperatorchecks /mnt/jail /usr/bin/env \
    PARTITION="$PARTITION" \
    JQ_EXCLUDE_BAD_NODES="$JQ_EXCLUDE_BAD_NODES" \
    ACTIVE_CHECK_NAME="$ACTIVE_CHECK_NAME" /bin/bash <<'EOF'
NUM_NODES=0
for node in $(sinfo -N --partition="$PARTITION" --responding --json | jq -r "$JQ_EXCLUDE_BAD_NODES"); do
    (
        flock 9
        echo "Updating node: $node"
        extra_json=$(scontrol show node "$node" | awk -F= '/Extra=/{print $2}')
        if [[ -z "$extra_json" || "$extra_json" == "none" ]]; then
            extra_json="{}"
        fi
        updated_json=$(echo "$extra_json" | jq -c --arg key "$ACTIVE_CHECK_NAME" --argjson val true '.[$key] = $val')
        sudo scontrol update NodeName="$node" Extra="$updated_json"
        exit 0
    ) 9>"/etc/soperatorchecks/active_check_${node}.lock" && NUM_NODES=$(( NUM_NODES + 1 ))
done

echo $NUM_NODES > /etc/soperatorchecks/$ACTIVE_CHECK_NAME.num_nodes
EOF

NUM_NODES=$(cat /mnt/jail/etc/soperatorchecks/$ACTIVE_CHECK_NAME.num_nodes)

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
        --array=0-$((ARRAY_SIZE-1)) \
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
