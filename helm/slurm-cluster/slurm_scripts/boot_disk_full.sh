#!/bin/bash

set -eox

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
    echo "$usage% of space is used. Please try to clean up volumes from '/opt/soperator_utils/fs_usage.sh -l', \
delete leftover containers from 'enroot list' and 'docker ps -a', \
or reboot the node via 'scontrol reboot'" >&3
    exit 1
fi

exit 0
