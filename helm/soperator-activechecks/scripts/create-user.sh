set -ex

CLUSTER_NAME="{{ .Values.slurmClusterRefName }}"
NAMESPACE="{{ .Release.Namespace }}"
HEADLESS_SVC="${CLUSTER_NAME}-login-headless-svc.${NAMESPACE}.svc.cluster.local"
SSH_KEY="/mnt/jail/opt/soperator-home/soperatorchecks/.ssh/soperatorchecks_id_ecdsa"
LOGIN_HOST="login-0.${HEADLESS_SVC}"

echo "Creating ${USER_NAME} user..."

retry -d 2 -t 10 -- ssh -i "${SSH_KEY}" \
    -o StrictHostKeyChecking=no \
    soperatorchecks@"${LOGIN_HOST}" \
    "id '${USER_NAME}' || echo '' | sudo soperator-createuser '${USER_NAME}' --gecos '' --home /opt/soperator-home/'${USER_NAME}'"

# Because of the bug in filestore ssh is unavailable for ~15 sec after new user creation.
echo "Wait for ssh availability 20 sec..."
sleep 20
