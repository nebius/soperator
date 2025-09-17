set -euxo pipefail

echo "Cleaning old Soperator outputs"

find /mnt/jail/opt/soperator-outputs -type f -mmin +30 -delete
