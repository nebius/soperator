#!/bin/bash

helm pull oci://cr.ai.nebius.cloud/yc-marketplace/nebius/gpu-operator/chart/gpu-operator \
  --version v23.9.0

helm install --kube-context ncp-slurm-operator gpu-operator ./gpu-operator-v23.9.0.tgz \
  --namespace nvidia-gpu-operator \
  --create-namespace \
  --set toolkit.enabled=true \
  --set driver.upgradePolicy.autoUpgrade=false \
  --set driver.rdma.enabled=true \
  --set driver.version=535.104.12 \
  --wait
