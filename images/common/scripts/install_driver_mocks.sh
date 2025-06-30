#!/bin/bash

set -e

# Install mock packages for nvidia drivers https://github.com/nebius/soperator/issues/384

# Meta packages:
#   - cuda-drivers (old)
#   - nvidia-open (new, >= 560)
# Specific packages:
#   - libnvidia-compute-*

driver_packages=(
  cuda-drivers
  nvidia-open
  libnvidia-compute-525
  libnvidia-compute-530
  libnvidia-compute-535
  libnvidia-compute-545
  libnvidia-compute-550
  libnvidia-compute-555
  libnvidia-compute-560
  libnvidia-compute-565
  libnvidia-compute-570
  libnvidia-compute-575
  libnvidia-compute-580
)

# Install deb package mocking tool
apt update -y && apt install -y equivs

# Build mock packages
mkdir -p /tmp/driver_mocks
pushd /tmp/driver_mocks
  echo "Generate mock package definitions"
  for pkg in "${driver_packages[@]}"; do
    cat > "mock_package.ctl" <<EOF
Package: $pkg
Version: 99999999-fake
Architecture: all
Provides: $pkg
Description: Fake NVIDIA driver package to block automatic installation inside containers due to dependency resolution
EOF
    MAKEFLAGS="-j$(nproc)" equivs-build mock_package.ctl
  done

  echo "Install mock packages"
  dpkg -i ./*.deb
popd || true

# Cleanup
rm -rf /tmp/driver_mocks
apt-get clean && rm -rf /var/lib/apt/lists/*
