#!/bin/bash

set -eox pipefail

echo "[$(date)] Check running services"

# The list of services (process names) that must run on the K8s node
required_services=(
    nvidia-persistenced
    nv-fabricmanager
)

for svc in "${required_services[@]}"; do
    svc_trimmed="${svc:0:15}"
    if chroot /run/nvidia/driver pgrep --ns 1 --nslist pid --exact "${svc_trimmed}" 2>/dev/null; then
        echo "Service ${svc} is running"
    else
        echo "Required service ${svc} is not running"
        # Return failure details
        echo "service ${svc} is not running" >&3
        exit 1
    fi
done

exit 0
