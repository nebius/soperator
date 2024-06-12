#!/bin/bash

helm repo add nvidia https://helm.ngc.nvidia.com/nvidia
helm repo update

helm install --kube-context yc-nemax-dev-poc --wait gpu-operator \
    -n gpu-operator --create-namespace \
    gpu-operator \
    --set toolkit.enabled=true \
    --set driver.upgradePolicy.autoUpgrade=false \
    --set driver.rdma.enabled=true \
    --set driver.nvidiaDriverCRD.enabled=true \
    --set driver.nvidiaDriverCRD.deployDefaultCR=false

#helm pull oci://cr.ai.nebius.cloud/yc-marketplace/nebius/gpu-operator/chart/gpu-operator \
#  --version v23.9.0
#
#helm install --kube-context yc-nemax-dev-poc gpu-operator ./gpu-operator-v23.9.0.tgz \
#  --namespace nvidia-gpu-operator \
#  --create-namespace \
#  --set toolkit.enabled=true \
#  --set driver.upgradePolicy.autoUpgrade=false \
#  --set driver.rdma.enabled=true \
#  --set driver.version=470.223.02 \
#  --wait
