set -e

CLUSTER_NAME="{{ .Values.slurmClusterRefName }}"
NAMESPACE="{{ .Release.Namespace }}"
HEADLESS_SVC="${CLUSTER_NAME}-login-headless-svc.${NAMESPACE}.svc.cluster.local"
SSH_KEY="/mnt/jail/opt/soperator-home/soperatorchecks/.ssh/soperatorchecks_id_ecdsa"

if kubectl get pod -n "${NAMESPACE}" "${CLUSTER_NAME}-login-0" >/dev/null 2>&1; then
  POD_PREFIX="${CLUSTER_NAME}-"
elif kubectl get pod -n "${NAMESPACE}" "login-0" >/dev/null 2>&1; then
  POD_PREFIX=""
else
  echo "No login pods found (neither ${CLUSTER_NAME}-login-0 nor login-0 exists in ${NAMESPACE})"
  exit 1
fi

JUMPHOST="${POD_PREFIX}login-0.${HEADLESS_SVC}"

echo "Checking ssh connectivity to login nodes (prefix='${POD_PREFIX}')..."
for ((i=0; i<${NUM_OF_LOGIN_NODES}; i++)); do
  POD_HOST="${POD_PREFIX}login-${i}.${HEADLESS_SVC}"
  echo "Connecting to ${POD_HOST} via jumphost ${JUMPHOST}..."

  # If we have more than one login node and create user on one of them,
  # other node become unavailable for new SSH connections for around 10 seconds.
  retry -d 2 -t 10 -- ssh -i "${SSH_KEY}" \
      -o StrictHostKeyChecking=no \
      -o "ProxyCommand=ssh -i ${SSH_KEY} -o StrictHostKeyChecking=no -W %h:%p soperatorchecks@${JUMPHOST}" \
      soperatorchecks@"${POD_HOST}" hostname
done
