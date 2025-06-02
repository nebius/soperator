# Soperator Helm chart

This Helm chart deploys Soperator,
a Kubernetes operator designed to manage and run Slurm clusters within Kubernetes environments.

## Prerequisites

Before deploying Soperator, ensure the following prerequisites are met:

- **Kubernetes Cluster**: A running Kubernetes cluster, version 1.30 or higher.
- **Helm**: Helm package manager installed.
- **NVIDIA GPU Operator**: Installed if utilizing NVIDIA GPUs.
- **NVIDIA Network Operator**: Installed if using InfiniBand networking.

### OpenKruise

Soperator relies on [**OpenKruise operator**](https://github.com/openkruise/charts/tree/master/versions/kruise/1.8.0)
to manage **Advanced StatefulSets**.

By default, it's installed within this chart.
However, you can disable its installation if you already have OpenKruise operator installed in your cluster.

> [!IMPORTANT]
> Make sure you have required feature gates stated in [values.yaml](./values.yaml)/`kruise.featureGates`
> opened in case of self-installation.

## Installation

To install the Soperator Helm chart, follow these steps:

### Experimental OCI

Make sure you have experimental OCI-based registries:
```bash
export HELM_EXPERIMENTAL_OCI=1
````

### Add the Helm Repository

For the stable version:
```bash
helm repo add soperator oci://cr.eu-north1.nebius.cloud/soperator
```

For the dev version:
```bash
helm repo add soperator-dev oci://cr.eu-north1.nebius.cloud/soperator-unstable
```

### Update Helm Repositories

```bash
helm repo update
```

### Install Soperator

```bash
helm install soperator soperator[-dev]/helm-soperator --namespace soperator-system --create-namespace
```
