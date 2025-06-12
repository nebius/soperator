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

echo "Creating epilog script..."
EPILOG_SCRIPT=$(mktemp /tmp/activecheck-epilog.XXXXXX.sh)
chmod +x "$EPILOG_SCRIPT"

cat <<EOF > "$EPILOG_SCRIPT"
#!/bin/bash
ACTIVE_CHECK_NAME="$ACTIVE_CHECK_NAME"
NODE_NAME=\$(hostname)

echo "Running embedded epilog on node: \$NODE_NAME"

extra_json=\$(scontrol show node "\$NODE_NAME" | awk -F= '/Extra=/{print \$2}')
if [[ -z "\$extra_json" || "\$extra_json" == "none" ]]; then
    extra_json="{}"
fi
updated_json=\$(echo "\$extra_json" | jq -c --arg key "\$ACTIVE_CHECK_NAME" --argjson val false '.[\$key] = \$val')
scontrol update NodeName="\$NODE_NAME" Extra="\$updated_json"

echo "Epilog completed for \$NODE_NAME"
EOF

HOSTS_NUM=$(sinfo -N --noheader -o "%N" | wc -l)

echo "Submitting Slurm job..."
SLURM_OUTPUT=$(/usr/bin/sbatch --parsable --export=ALL,HOSTS_NUM,EPILOG_SCRIPT /opt/bin/sbatch.sh)

if [[ -z "$SLURM_OUTPUT" ]]; then
    echo "Failed to submit Slurm job"
    exit 1
fi

SLURM_JOB_ID="$SLURM_OUTPUT"
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
