#!/bin/bash

helm repo add nvidia https://helm.ngc.nvidia.com/nvidia
helm repo update

helm install --kube-context ncp-slurm-operator network-operator nvidia/network-operator \
    -n nvidia-network-operator \
    --create-namespace \
    --version v23.7.0 \
    -f ./network-operator-values.yaml \
    --wait
