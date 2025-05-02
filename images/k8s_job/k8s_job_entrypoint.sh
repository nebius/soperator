#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code

echo "Link users from jail"
ln -s /mnt/jail/etc/passwd /etc/passwd
ln -s /mnt/jail/etc/group /etc/group
ln -s /mnt/jail/etc/shadow /etc/shadow
ln -s /mnt/jail/etc/gshadow /etc/gshadow
chown -h 0:42 /etc/{shadow,gshadow}

echo "Link home from jail to use SSH keys from there"
ln -s /mnt/jail/home /home

echo "Complement jail rootfs"
/opt/bin/slurm/complement_jail.sh -j /mnt/jail -u /mnt/jail.upper

echo "Symlink slurm configs from jail(sconfigcontroller)"
rm -rf /etc/slurm && ln -s /mnt/jail/slurm /etc/slurm

echo "Submitting Slurm job..."
SLURM_OUTPUT=$(sbatch --parsable --wrap="/opt/bin/sbatch.sh")

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
