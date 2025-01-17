#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code
set -x # Print actual command before executing it

echo "Evaluate variables in the Slurm node 'Extra' field"
evaluated_extra=$(eval echo "$SLURM_NODE_EXTRA")

echo "Start slurmd daemon"
exec /usr/sbin/slurmd \
  -D \
  -Z \
  --instance-id "${INSTANCE_ID}" \
  --extra "${evaluated_extra}" \
  --conf \
  "NodeHostname=${K8S_POD_NAME} NodeAddr=${K8S_POD_NAME}.${K8S_SERVICE_NAME}.${K8S_POD_NAMESPACE}.svc.cluster.local RealMemory=${SLURM_REAL_MEMORY} Gres=${GRES}" \
  2>&1 | tee >(multilog s100000000 n5 /var/log/slurm/multilog)
