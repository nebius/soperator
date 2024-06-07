#!/bin/bash

helm repo add nvidia https://helm.ngc.nvidia.com/nvidia
helm repo update

helm install --kube-context ncp-slurm-operator --wait gpu-operator \
    -n gpu-operator --create-namespace \
    nvidia/gpu-operator \
    --set driver.nvidiaDriverCRD.enabled=true \
    --set driver.nvidiaDriverCRD.deployDefaultCR=false
