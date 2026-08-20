set -ex

# Slurm reads topology.yaml when it exists and ignores topology.conf, so either file means the
# topology has been delivered.
TOPOLOGY_CONF="/mnt/jail/etc/slurm/topology.conf"
TOPOLOGY_YAML="/mnt/jail/etc/slurm/topology.yaml"

echo "Waiting for $TOPOLOGY_YAML or $TOPOLOGY_CONF..."

while [ ! -s "$TOPOLOGY_YAML" ] && [ ! -s "$TOPOLOGY_CONF" ]; do
    sleep 5
done

if [ -s "$TOPOLOGY_YAML" ]; then
    echo "$TOPOLOGY_YAML is present."
else
    echo "$TOPOLOGY_CONF is present."
fi
