# Soperator – Kubernetes Operator for Slurm
[//]: # (Badges)
<div align="center">

[![tag-machine-learning](https://img.shields.io/badge/machine_learning-blue?style=for-the-badge)](#)
[![tag-model-training](https://img.shields.io/badge/model_training-deepskyblue?style=for-the-badge)](#)
[![tag-high-performance-computing](https://img.shields.io/badge/high--performance_computing-lightseagreen?style=for-the-badge)](#)

</div>

<div align="center">

[![github-last-commit](https://img.shields.io/github/last-commit/nebius/soperator?style=for-the-badge)](#)
[![github-license](https://img.shields.io/github/license/nebius/soperator?style=for-the-badge)](#-license)

</div>

[//]: # (Short description)
Run Slurm in Kubernetes and enjoy the benefits of both systems. You can learn more about Soperator, its prerequisites,
and architecture in the [Medium article](https://medium.com/p/e7a41f307d14).

<img src="docs/images/layers_diagram.png" alt="Slurm in Kubernetes" width="100%" height="auto"/>


## 📋 Table of contents
- [💡 Rationale](#-rationale)
- [⭐ Features](#-features)
- [❌ Limitations](#-limitations)
- [🚀 Installation](#-installation)
- [📈 Future plans](#-future-plans)
- [📚 Documentation](#-documentation)
- [🤬 Feedback](#-feedback)
- [🤝 Contribution](#-contribution)
- [🏛 License](#-license)



## 💡 Rationale
Both [Slurm](https://slurm.schedmd.com/overview.html) and [Kubernetes](https://kubernetes.io/docs/concepts/overview/)
can serve as workload managers for distributed model training and high-performance computing (HPC) in general.

Each of these systems has its strengths and weaknesses, and the trade-offs between them are significant. Slurm offers
advanced and effective scheduling, granular hardware control, and accounting, but lacks universality. On the other hand,
Kubernetes can be used for purposes other than training (e.g. inference) and provides good auto-scaling and self-healing
capabilities. For a detailed comparison, see the [Nebius AI blog post](https://nebius.ai/blog/posts/model-pre-training/slurm-vs-k8s).

It's unfortunate that there is no way to combine the benefits of both solutions. And since many big tech companies use
Kubernetes as their default infrastructure layer without supporting a dedicated model training system, some ML engineers
don't even have a choice.

That's why we decided to marry these systems, taking a "Kubernetes-first" approach. We implemented a [Kubernetes
operator](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/), which is a software component that runs
and manages Slurm clusters as Kubernetes resources.

<img src="docs/images/architecture_diagram.svg" alt="Solution Architecture" width="100%" height="auto"/>

This allowed us to reuse the autoscaling and self-healing of Kubernetes in Slurm, and implement some unique features,
while maintaining the usual way of interacting with it.



## ⭐ Features


### Shared root filesystem
When users interact with a Slurm cluster, they see a shared filesystem as their root "**/**" directory. With this
approach, you can keep using Slurm in a familiar way (e.g. you don't need to run all jobs in containers).

It also means that you don't have to keep nodes in an identical state. Changes that you make on one node—e.g.
install new software packages, create Linux users, write job outputs, or download datasets—can be **immediately
reflected on all other nodes**.


### GPU health checks
If your Kubernetes cluster contains NVIDIA GPUs, the operator will perform regular GPU health checks. If a Slurm
node fails a health check, the operator “drains” it, so that new jobs are not scheduled on it.


### Easy scaling
Each stage of building an ML product has its own requirements for computing resources.

Soperator allows Slurm to reuse the unique Kubernetes feature of scaling automatically according to current needs.
You can simply change a single value in the YAML manifest and watch the cluster change in size.


### High availability
Kubernetes comes with some level of HA out of the box. If a pod or container, such as a Slurm controller, fails,
Kubernetes recreates it.

Soperator takes this even further, continuously bringing the entire cluster up to the desired state.


### Isolation of user actions
All user actions are isolated within a dedicated container-like environment, so that an action can't break the
Slurm cluster itself by accident. This defines a clear boundary between operator and user responsibility.

### Tailscale support
You can optionally enable [Tailscale](https://github.com/tailscale/tailscale) as a sidecar on login pods and access the cluster securely from your own network.

### Accounting
Slurm's accounting system records detailed job information such as:

- CPU and memory consumption
- User and group identities
- Job start/end times
- Resource requests and allocations
- If `protectedSecret` is set to `true`, the user secret for MariaDB will not be deleted after the MariaDB CR is deleted

This helps cluster administrators and users monitor resource utilization, enforce quotas, and generate usage reports for performance optimization or billing purposes.


## ❌ Limitations
- **GPU-only or CPU-only**.The cluster of Slurm can currently be either GPU-only or CPU-only. 
  Support for mixed configurations based on nodesets (e.g., separate GPU and CPU nodesets) has not been implemented yet.
- **Single-partition clusters**. Slurm's ability to split clusters into several partitions isn't supported now.
- **Software versions**. The list of software versions we currently support is quite short.
    - Linux: Ubuntu [24.04](https://releases.ubuntu.com/noble/).
    - Slurm: versions `25.11.2`.
    - CUDA: version [12.9](https://docs.nvidia.com/cuda/archive/12.9.0/cuda-toolkit-release-notes/contents.html).
    - Kubernetes: >= [1.31](https://kubernetes.io/blog/2024/08/13/kubernetes-v1-31-release/).
    - Versions of some preinstalled software packages can't be changed.


## 🚀 Installation
The steps required to deploy Soperator to your Kubernetes cluster depend on whether you are using Kubernetes
on premises or in a cloud.


### Nebius AI
For [Nebius AI](https://nebius.ai/), we provide a Terraform recipe that creates everything itself, which includes:
- A [Managed Kubernetes](https://nebius.ai/docs/managed-kubernetes) cluster.
- A [virtual network](https://nebius.ai/docs/vpc) and public IP addresses.
- At least one shared [file storage](https://nebius.ai/docs/compute/concepts/filesystem) that stores your environment.
  File storages are distributed filesystems focused on concurrent reads and writes.

Everything specific to Nebius AI is contained in a separate repository:
[nebius/nebius-solutions-library](https://github.com/nebius/nebius-solutions-library/tree/main/soperator).

[//]: #


### Other clouds and on-premises
> [!IMPORTANT]
> When using the soperator, it is important that the CNI supports preserving the client source IP.
> Therefore, if kube-proxy is configured in IPVS mode, or if you're using CNI plugins like kube-router or Antrea Proxy,
the operator will not work.
> This operator has been tested with
the [Cilium network plugin](https://kubernetes.io/docs/concepts/extend-kubernetes/compute-storage-net/network-plugins/)
> running in
[kube-proxy replacement mode](https://docs.cilium.io/en/stable/network/kubernetes/kubeproxy-free/#kubernetes-without-kube-proxy).

In general, you need to follow these steps:
1. Decide on the shared storage technology you would like to use. At least one shared filesystem is necessary, because
   it will store the environment shared by Slurm nodes. The only thing the Soperator requires is the
   [PVC](https://kubernetes.io/docs/concepts/storage/persistent-volumes/) name. Consider using
   [NFS](https://kubernetes.io/docs/concepts/storage/volumes/#nfs) as the simplest option, or something more advanced
   like [OpenEBS](https://openebs.io/) or [GlusterFS](https://www.gluster.org/).
2. Install the [NVIDIA GPU Operator](https://github.com/NVIDIA/gpu-operator).
3. If you use InfiniBand, install the [NVIDIA Network Operator](https://github.com/Mellanox/network-operator).
4. Install Soperator by applying the [soperator](helm/soperator) Helm chart.
5. Create a Slurm cluster in a namespace with the same name as the slurm cluster by 
   applying the [slurm-cluster](helm/slurm-cluster) Helm chart.
6. Wait until the `slurm.nebius.ai/SlurmCluster` resource becomes `Available`.

[//]: # (TODO: Refer to Helm OCI images instead of file directories when the repo is open)

> [!WARNING]
> Although Soperator should be compatible with any Kubernetes installation in principle, we haven't tested
> it anywhere outside Nebius AI, so it's likely that something won't work out of the box or will require additional
> configuration. If you're facing issues, create an issue in this repository, and we will help you install Soperator
> to your Kubernetes and update these docs.



## 📈 Future plans
- 💡 **On-demand nodes**. The easy scaling can be improved further by provisioning new Kubernetes nodes only when
  there are queued jobs that need them.
- 💡 **Network topology-aware job scheduling**. Thanks to the Slurm topology feature, we can support detailed
  configuration of the network topology for more efficient scheduling.
- 💡 **Automatic replacement of bad-performing nodes**. Currently, Soperator just drains the Slurm nodes that have
  problems. We have a plan to replace such nodes automatically.
- 💡 **More system checks**. Soperator only checks GPUs at the moment, but there are more things to check: software
  issues, storage performance, network connectivity, etc. So we're going to continue adding new checks.
- 💡 **Jail backups**. This means backing up the shared storage to improve durability.
- 💡 **Automatic external checkpointing**. We consider using NVIDIA's
  [cuda-checkpoint](https://github.com/NVIDIA/cuda-checkpoint) for dumping and resuming job processes externally.



## 📚 Documentation
The detailed documentation is located in the [docs](docs) directory of this repository.

It includes, among other things:
- A detailed description of the Soperator [architecture](docs/architecture.md).
- The [full list of features](docs/features.md) that this solution provides comparing to typical Slurm installations.
- A more complete description of the existing [limitations](docs/limitations.md).
- [Local development with Kind](docs/local-development.md) - setting up a local Kubernetes cluster for development and testing.
- The [release process](docs/release-process.md) for both soperator and nebius-solutions-library repositories.
- [Metrics collection and processing](docs/metrics-pipeline.md) documentation.
- [Logs collection and aggregation](docs/logs-pipeline.md) pipeline guide.



## 🤬 Feedback
If you like this project, **star it on GitHub**. So we will see that the community is interested in it and continue
developing it further, openly and publicly.

If you failed to install Soperator to your Kubernetes cluster or encounter any other issue with it, create a
[GitHub issue](https://github.com/nebius/soperator/issues) and write details about your problem. We will try to help.

> [!NOTE]
> This project is very new and quite raw - it was started in May 2024. And if it already works stably in Nebius AI,
> this may not be the case for other clouds.



## 🤝 Contribution
Unfortunately, at the moment we don't have development docs for outside developers who want to participate in this
project. If you are interested in contribution, create a GitHub issue, and we'll figure something out.

Also, pay attention to the list of future plans we have. The tasks we are currently working on are marked there. Maybe
you need just one of these.



## 🏛 License
The Soperator itself is licensed under [Apache 2.0](LICENSE), a permissive free software license that allows you to use
the software for any purpose, to distribute it, to modify it, and to distribute modified versions under specific terms.

Please note that various pieces of software it installs in your cluster may have other licenses.
