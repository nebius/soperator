set -euxo pipefail

echo "Cleaning old Soperator outputs (health check logs)"

chroot /mnt/jail /bin/sh -c "find /opt/soperator-outputs -type f -mtime +7 -delete"
