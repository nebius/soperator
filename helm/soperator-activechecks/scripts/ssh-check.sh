set -e

apt update && apt install -y retry

echo "Checking ssh connectivity to login nodes..."
for ((i=0; i<${NUM_OF_LOGIN_NODES}; i++)); do
  echo "Connecting to node login-$i via jumphost..."

  # If we have more than one login node and create user on one of them,
  # other node become unavailable for new SSH connections for around 10 seconds.
  retry -d 2 -t 10 -- ssh -i /mnt/jail/opt/soperator-home/soperatorchecks/.ssh/soperatorchecks_id_ecdsa \
      -o StrictHostKeyChecking=no \
      -o "ProxyCommand=ssh -i /mnt/jail/opt/soperator-home/soperatorchecks/.ssh/soperatorchecks_id_ecdsa -o StrictHostKeyChecking=no -W %h:%p soperatorchecks@login-0.soperator-login-headless-svc.soperator.svc.cluster.local " \
      soperatorchecks@login-$i hostname
done
