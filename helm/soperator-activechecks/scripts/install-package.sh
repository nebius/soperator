set -e

echo "Installing libcudnn8"

# If we have more than one login node and create user on one of them,
# other node become unavailable for new SSH connections for around 10 seconds.
retry -d 2 -t 10 -- ssh -i /mnt/jail/opt/soperator-home/soperatorchecks/.ssh/soperatorchecks_id_ecdsa \
    -o StrictHostKeyChecking=no \
    soperatorchecks@login-0.soperator-login-headless-svc.soperator.svc.cluster.local \
    'flock /var/lock/apt.lock bash -c "sudo apt update && sudo apt install -y libcudnn8"'
