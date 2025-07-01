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

echo "Bind-mount /opt/bin/sbatch.sh script"
mount --bind /opt/bin/sbatch.sh opt/bin/sbatch.sh

echo "Create directory for slurm job outputs for every node"
NODES=$(sinfo -N --noheader -o "%N" | sort -u)
BASE_DIR="/mnt/jail/opt/soperator-outputs"
for NODE in $NODES; do
    DIR="${BASE_DIR}/${NODE}/slurm_jobs"
    (umask 000; mkdir -p "$DIR")
done

if [[ "$EACH_WORKER_JOB_ARRAY" == "true" ]]; then
    echo "Submitting job using slurm_submit_array_job.sh..."
    SLURM_JOB_ID=$(/opt/bin/slurm/slurm_submit_array_job.sh | tail -n 1)
else
    echo "Submitting regular Slurm job..."
    OUT_PATTERN='/opt/soperator-outputs/%N/slurm_jobs/%x.%j.out'
    # Here we use env variables instead of --output and --error because they do not support %N (node name) parameter.
    SLURM_OUTPUT=$(
      SBATCH_OUTPUT="$OUT_PATTERN" \
      SBATCH_ERROR="$OUT_PATTERN" \
      /usr/bin/sbatch --parsable \
        --job-name="$ACTIVE_CHECK_NAME" \
        --chdir=/opt/soperatorchecks \
        --uid=soperatorchecks \
        /opt/bin/sbatch.sh
    )
    if [[ -z "$SLURM_OUTPUT" ]]; then
        echo "Failed to submit Slurm job"
        exit 1
    fi
    SLURM_JOB_ID="$SLURM_OUTPUT"
fi

echo "Slurm Job ID: $SLURM_JOB_ID"

echo "Looking up owning Job for pod: $K8S_POD_NAME in namespace: $K8S_POD_NAMESPACE"
K8S_JOB_NAME=$(kubectl get pod "$K8S_POD_NAME" -n "$K8S_POD_NAMESPACE" -o jsonpath='{.metadata.ownerReferences[?(@.kind=="Job")].name}')

if [[ -z "$K8S_JOB_NAME" ]]; then
    echo "Could not find owning Job for pod: $K8S_POD_NAME"
    exit 1
fi

echo "Annotating Job $K8S_JOB_NAME with slurm-job-id=$SLURM_JOB_ID"
kubectl annotate job "$K8S_JOB_NAME" slurm-job-id="$SLURM_JOB_ID" \
    -n "$K8S_POD_NAMESPACE" --overwrite || {
    echo "Failed to annotate Job"
    exit 1
}
