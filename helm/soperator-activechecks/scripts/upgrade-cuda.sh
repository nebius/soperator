set -e

apt update && apt install -y retry

echo "Upgrading cuda"

# If we have more than one login node and create user on one of them,
# other node become unavailable for new SSH connections for around 10 seconds.
retry -d 2 -t 10 -- ssh -i /mnt/jail/opt/soperator-home/soperatorchecks/.ssh/soperatorchecks_id_ecdsa \
    -o StrictHostKeyChecking=no \
    soperatorchecks@login-0.soperator-login-headless-svc.soperator.svc.cluster.local << 'EOF'

# Patterns of packages to process
TARGET_PATTERNS=(
  "libcublas"
  "libcudnn"
  "libnccl"
)

echo "Unholding previously pinned CUDA-related libraries..."
for pattern in "${TARGET_PATTERNS[@]}"; do
  pkgs=$(dpkg -l | awk '{print $2}' | grep "^${pattern}" || true)
  for pkg in $pkgs; do
    if apt-mark showhold | grep -q "^$pkg$"; then
      echo "Unholding $pkg"
      sudo apt-mark unhold -o DPkg::Lock::Timeout=-1 "$pkg"
    fi
  done
done

echo "Removing existing CUDA installation..."
sudo apt remove -o DPkg::Lock::Timeout=-1 -y cuda || true

echo "Installing CUDA version ${CUDA_VERSION}..."
sudo apt -o DPkg::Lock::Timeout=-1 update
sudo apt install -o DPkg::Lock::Timeout=-1 -y "cuda=${CUDA_VERSION}"

sudo apt clean
sudo apt autoremove -y

echo "Holding newly installed CUDA-related libraries..."
for pattern in "${TARGET_PATTERNS[@]}"; do
  pkgs=$(dpkg -l | awk '{print $2}' | grep "^${pattern}" || true)
  for pkg in $pkgs; do
    if dpkg -s "$pkg" &>/dev/null; then
      echo "Holding $pkg"
      sudo apt-mark hold -o DPkg::Lock::Timeout=-1 "$pkg"
    fi
  done
done

echo "CUDA ${CUDA_VERSION} installation complete and relevant packages pinned (held)."
