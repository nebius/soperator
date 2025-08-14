#!/bin/bash

set -euxo pipefail

echo "[$(date)] Check if node boot disk is full"

THRESHOLD=80

echo "Get the usage percentage and strip the '%'"
read -r usage < <(/bin/df -P "/" | awk 'NR==2 { sub(/%/,"",$5); print $5 }' || echo '')

if [[ -z "${usage}" ]]; then
    echo "Could not determine boot disk usage" >&2
    exit 0
fi

echo "Node boot disk is ${usage}% full (threshold $THRESHOLD%)"

if [ "$usage" -gt "$THRESHOLD" ]; then
    # Return failure details
    echo "$usage% of node boot disk is used. \
Clean up volumes from 'ssh $SLURMD_NODENAME /opt/soperator_utils/fs_usage.sh -l', \
delete leftover containers from 'ssh $SLURMD_NODENAME enroot list' and 'ssh $SLURMD_NODENAME docker ps -a', \
reboot the node using 'scontrol reboot $SLURMD_NODENAME', \
or stop-start the InstanceId from 'scontrol show node $SLURMD_NODENAME'" >&3
    exit 1
fi

exit 0
