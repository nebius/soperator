set -ex

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

LOGIN_HOST="${POD_PREFIX}login-0.${HEADLESS_SVC}"

echo "Creating ${USER_NAME} user..."

retry -d 2 -t 10 -- ssh -i "${SSH_KEY}" \
    -o StrictHostKeyChecking=no \
    soperatorchecks@"${LOGIN_HOST}" \
    "id '${USER_NAME}' || echo '' | sudo soperator-createuser '${USER_NAME}' --gecos '' --home /opt/soperator-home/'${USER_NAME}'"

# Because of the bug in filestore ssh is unavailable for ~15 sec after new user creation.
echo "Wait for ssh availability 20 sec..."
sleep 20
