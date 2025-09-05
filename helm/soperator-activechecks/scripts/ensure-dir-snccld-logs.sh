set -ex

echo "Ensuring log directory for NCCL Debug plugin (${SNCCLD_LOG_DIR_PATH})..."
chroot /mnt/jail /bin/bash -c "mkdir -p '${SNCCLD_LOG_DIR_PATH}'; chmod 777 '${SNCCLD_LOG_DIR_PATH}'"
