set -euxo pipefail

echo "Cleaning old Soperator outputs (health check logs) if last modify > 4 hours"

/bin/sh -c "find /mnt/jail/opt/soperator-outputs -type f -mmin +240 -delete"
