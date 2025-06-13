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
updated_json=\$(echo "\$extra_json" | jq -c --arg key "\$ACTIVE_CHECK_NAME" 'del(.["\$\key"])')
scontrol update NodeName="\$NODE_NAME" Extra="\$updated_json"

echo "Epilog completed for \$NODE_NAME"
EOF

echo "Submitting Slurm array job..."
HOSTS_NUM=$(sinfo -N --noheader -o "%N" | wc -l)
SLURM_OUTPUT=$(/usr/bin/sbatch --parsable --export=ALL,EPILOG_SCRIPT --extra="${ACTIVE_CHECK_NAME}=true" --array=0-$((HOSTS_NUM - 1)) --nodes=1 /opt/bin/sbatch.sh)

if [[ -z "$SLURM_OUTPUT" ]]; then
    echo "Failed to submit Slurm job"
    exit 1
fi

echo "$SLURM_OUTPUT"
