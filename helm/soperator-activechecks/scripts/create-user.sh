set -ex

echo "Creating ${USER_NAME} user..."

retry -d 2 -t 10 -- ssh -i /mnt/jail/opt/soperator-home/soperatorchecks/.ssh/soperatorchecks_id_ecdsa \
    -o StrictHostKeyChecking=no \
    soperatorchecks@login-0.soperator-login-headless-svc.soperator.svc.cluster.local \
    "id '${USER_NAME}' || echo '' | sudo soperator-createuser '${USER_NAME}' --gecos '' --home /opt/soperator-home/'${USER_NAME}'"

# Because of the bug in filestore ssh is unavailable for ~15 sec after new user creation.
echo "Wait for ssh availability 20 sec..."
sleep 20
