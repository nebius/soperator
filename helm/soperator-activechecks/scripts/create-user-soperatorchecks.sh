set -ex

echo "Creating soperatorchecks user..."
chroot /mnt/jail /bin/bash -c 'id "soperatorchecks" || echo "" | createuser soperatorchecks --home /opt/soperator-home/soperatorchecks --gecos ""'

if [ ! -f /mnt/jail/opt/soperator-home/soperatorchecks/.ssh/soperatorchecks_id_ecdsa ]; then
  echo "Generating ssh key..."
  ssh-keygen -t ecdsa -f /mnt/jail/opt/soperator-home/soperatorchecks/.ssh/soperatorchecks_id_ecdsa -N '' -C soperatorchecks
  cat /mnt/jail/opt/soperator-home/soperatorchecks/.ssh/soperatorchecks_id_ecdsa.pub >> /mnt/jail/opt/soperator-home/soperatorchecks/.ssh/authorized_keys
fi
