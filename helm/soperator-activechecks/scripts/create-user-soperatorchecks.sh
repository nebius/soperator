set -ex

echo "Creating soperatorchecks user..."
chroot /mnt/jail /bin/bash -s <<'EOF'

id "soperatorchecks" || echo "" | soperator-createuser soperatorchecks --home /opt/soperator-home/soperatorchecks --gecos ""

if [ ! -f /opt/soperator-home/soperatorchecks/.ssh/soperatorchecks_id_ecdsa ]; then
  echo "Generating ssh key..."
  ssh-keygen -t ecdsa -f /opt/soperator-home/soperatorchecks/.ssh/soperatorchecks_id_ecdsa -N '' -C soperatorchecks
  cat /opt/soperator-home/soperatorchecks/.ssh/soperatorchecks_id_ecdsa.pub >> /opt/soperator-home/soperatorchecks/.ssh/authorized_keys
fi

mkdir -p /etc/soperatorchecks
chown soperatorchecks:soperatorchecks /etc/soperatorchecks

EOF
