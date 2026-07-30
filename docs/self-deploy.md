# Self-deploying Soperator on any Kubernetes

Follow the steps below to deploy Soperator on Kubernetes clusters outside Nebius AI, including on-premises environments and other cloud providers.

## Networking requirement

> [!IMPORTANT]
> When using Soperator, it is important that the CNI supports preserving the client source IP.
> Therefore, if `kube-proxy` is configured in IPVS mode, or if you're using CNI plugins like kube-router or Antrea Proxy,
> the operator will not work.
>
> This operator has been tested with the [Cilium network plugin](https://kubernetes.io/docs/concepts/extend-kubernetes/compute-storage-net/network-plugins/)
> running in [kube-proxy replacement mode](https://docs.cilium.io/en/stable/network/kubernetes/kubeproxy-free/#kubernetes-without-kube-proxy).

## Installation

1. Decide on the shared storage technology you would like to use. At least one shared filesystem is necessary, because it stores the environment shared by Slurm nodes. The only thing Soperator requires is the name of the [PVC](https://kubernetes.io/docs/concepts/storage/persistent-volumes/). Consider using [NFS](https://kubernetes.io/docs/concepts/storage/volumes/#nfs) as the simplest option, or something more advanced like [OpenEBS](https://openebs.io/) or [GlusterFS](https://www.gluster.org/).
2. Install the [NVIDIA GPU Operator](https://github.com/NVIDIA/gpu-operator).
3. If you use InfiniBand, install the [NVIDIA Network Operator](https://github.com/Mellanox/network-operator).
4. Install Soperator by applying the [`helm/soperator`](../helm/soperator) Helm chart.
5. Create a Slurm cluster in a namespace with the same name as the Slurm cluster by applying the [`helm/slurm-cluster`](../helm/slurm-cluster) Helm chart.
6. Wait until the `slurm.nebius.ai/SlurmCluster` resource becomes `Available`.

## Notes and limitations

> [!WARNING]
> Although Soperator should be compatible with any Kubernetes installation in principle, we haven't tested it anywhere outside Nebius AI, so it's likely that something won't work out of the box or will require additional configuration.
>
> If you're facing issues, create an issue in this repository, and we will help you install Soperator to your Kubernetes and update these docs.
