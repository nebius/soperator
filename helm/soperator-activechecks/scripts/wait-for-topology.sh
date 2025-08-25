set -ex

TOPOLOGY_CONF="/mnt/jail/etc/slurm/topology.conf"

echo "Waiting for $TOPOLOGY_CONF..."

while [ ! -s "$TOPOLOGY_CONF" ]; do
    sleep 5
done

echo "$TOPOLOGY_CONF is present."
