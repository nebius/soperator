#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code

echo "Start sshd daemon"
/usr/sbin/sshd -D -e -f /mnt/ssh-configs/sshd_config
sleep 1

# TODO: Using example job just to test image now. Fix it later.
echo "Submitting Slurm job..."
SLURM_OUTPUT=$(sbatch --parsable --wrap="echo Hello from Slurm")

if [[ -z "$SLURM_OUTPUT" ]]; then
    echo "Failed to submit Slurm job"
    exit 1
fi

SLURM_JOB_ID="$SLURM_OUTPUT"
echo "Slurm Job ID: $SLURM_JOB_ID"

POD_NAME=$(hostname)
NAMESPACE="${POD_NAMESPACE}"

echo "Looking up owning Job for pod: $POD_NAME in namespace: $NAMESPACE"
JOB_NAME=$(kubectl get pod "$POD_NAME" -n "$NAMESPACE" -o jsonpath='{.metadata.ownerReferences[?(@.kind=="Job")].name}')

if [[ -z "$JOB_NAME" ]]; then
    echo "Could not find owning Job for pod: $POD_NAME"
    exit 1
fi

echo "Annotating Job $JOB_NAME with slurm-job-id=$SLURM_JOB_ID"
kubectl annotate job "$JOB_NAME" slurm-job-id="$SLURM_JOB_ID" \
    -n "$NAMESPACE" --overwrite || {
    echo "Failed to annotate Job"
    exit 1
}
