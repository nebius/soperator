set -euxo pipefail

echo "Cleaning old Soperator outputs"

sudo find /mnt/jail/opt/soperator-outputs -type f -mmin +30 -delete
