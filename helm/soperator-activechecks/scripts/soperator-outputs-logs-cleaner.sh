set -euxo pipefail

echo "Cleaning old Soperator outputs (health check logs) if last modify > 4 hours"

chroot /mnt/jail /bin/sh -c "find /opt/soperator-outputs -type f -mmin +240 -delete"
