#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code

PARTITION="background"

echo "Cancelling currently active jobs with the same name..."
scancel --partition="$PARTITION" --name="$ACTIVE_CHECK_NAME"

# - List reservations and filter by name starting with reservationPrefix.
#   This is very safe and prevents us from interacting with reservations made by the customer.
# - Each reservation already has one node, so we don't need array .
#   We just run sbatch --reservation=<reservation-name> active_check.sh for each reservation.

echo "Submitting Slurm array job..."
for reservationName in $(scontrol show reservation --json | jq '.reservations.[] | .name'); do
sbatch --reservation=<reservation-name> 
    echo "Submitting regular Slurm job..."
    export SLURM_PROLOG="/etc/slurm/activecheck-prolog.sh"
    OUT_PATTERN='/opt/soperator-outputs/slurm_jobs/%N.%x.%j.out'
    # Here we use env variables instead of --output and --error because they do not support %N (node name) parameter.
    SLURM_OUTPUT=$(
      SBATCH_OUTPUT="$OUT_PATTERN" \
      SBATCH_ERROR="$OUT_PATTERN" \
      /usr/bin/sbatch --parsable \
        --job-name="$ACTIVE_CHECK_NAME" \
        --export="ALL,SLURM_PROLOG" \
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
        echo "Failed to submit Slurm job"
        exit 1
    fi
    SLURM_JOB_ID="$SLURM_JOB_ID $SLURM_OUTPUT"
done

# Trim spaces
SLURM_JOB_ID=$($SLURM_JOB_ID | xargs)

# Add comma separators
SLURM_JOB_ID=$($SLURM_JOB_ID | sed "s/ /,/g")

echo $SLURM_JOB_ID 
