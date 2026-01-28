#!/bin/bash

set -e # Exit immediately if any command returns a non-zero error code

feature_conf() {
    local current_host
    if ! current_host=$(hostname); then
        echo "Error: Failed to determine hostname" >&2
        return 1
    fi
    local features=()

    for var in $(env | grep "^SLURM_FEATURE_" | cut -d= -f1); do
        local feature_name=${var#SLURM_FEATURE_}
        local hostlist_expr=${!var}

        local expanded_hosts
        if ! expanded_hosts=$(scontrol show hostnames "$hostlist_expr"); then
            echo "Warning: Failed to expand hostlist expression '$hostlist_expr' for feature '$feature_name'" >&2
            continue
        fi

        if echo "$expanded_hosts" | grep -q "^$current_host$"; then
            features+=("$feature_name")
        fi
    done

    if [ ${#features[@]} -ne 0 ]; then
        local IFS=","
        echo " Feature=${features[*]} "
    fi
}

echo "Evaluate variables in the Slurm node 'Extra' field"
evaluated_extra=$(eval echo "$SLURM_NODE_EXTRA")

echo "Start slurmd daemon"

slurmd_args=(
  -D
  --instance-id "${INSTANCE_ID}"
)

if [ "${evaluated_extra}" != "" ]; then
  slurmd_args+=(
    --extra "${evaluated_extra}"
  )
fi

if [ "${SOPERATOR_NODE_SETS_ON}" = "true" ]; then
  echo "Running slurmd with NodeSets configuration from slurm.conf"

  # Default path should match consts.TopologyEnvFilePath used by the operator
  TOPOLOGY_ENV_FILE="${TOPOLOGY_ENV_FILE:-/tmp/topology/slurm_topology.env}"

  if [ -f "${TOPOLOGY_ENV_FILE}" ]; then
    echo "Loading topology from ${TOPOLOGY_ENV_FILE}"
    . "${TOPOLOGY_ENV_FILE}"
  else
    echo "WARNING: Topology env file not found at ${TOPOLOGY_ENV_FILE}"
    exit 2
  fi

  if [ -z "${SLURM_NODE_TOPOLOGY}" ]; then
    echo "ERROR: SLURM_NODE_TOPOLOGY is not set in ${TOPOLOGY_ENV_FILE}"
    exit 3
  fi

  slurmd_args+=(
    --conf
    "${SLURM_NODE_TOPOLOGY}"
  )
  
else
  echo "Running slurmd with dynamic node configuration"
  slurmd_args+=(
    -Z
    --conf
    "NodeHostname=${K8S_POD_NAME} NodeAddr=${K8S_POD_NAME}.${K8S_SERVICE_NAME}.${K8S_POD_NAMESPACE}.svc RealMemory=${SLURM_REAL_MEMORY} Gres=${GRES} $(feature_conf)"
  )
fi

exec /usr/sbin/slurmd "${slurmd_args[@]}" 2>&1 | tee >(multilog s100000000 n5 /var/log/slurm/multilog)
