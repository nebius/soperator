#!/bin/bash

echo "Start slurmd daemon"
exec /usr/sbin/slurmd \
  -D \
  -Z \
  --instance-id "${INSTANCE_ID}" \
  --conf \
  "NodeHostname=${K8S_POD_NAME} NodeAddr=${K8S_POD_NAME}.${K8S_SERVICE_NAME}.${K8S_POD_NAMESPACE}.svc.cluster.local RealMemory=${SLURM_REAL_MEMORY} Gres=${GRES}" \
  2>&1 | tee >(multilog s100000000 n5 /var/log/slurm/multilog)
