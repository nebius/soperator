# Soperator – Kubernetes Operator for Slurm
[//]: # (Badges)
[![tag-machine-learning](https://img.shields.io/badge/machine_learning-blue)](#)
[![tag-model-training](https://img.shields.io/badge/model_training-deepskyblue)](#)
[![tag-high-performance-computing](https://img.shields.io/badge/high--performance_computing-lightseagreen)](#)
<br/>
[![github-release](https://img.shields.io/github/v/release/nebius/soperator)](#)
[![github-release-date](https://img.shields.io/github/release-date/nebius/soperator)](#)
[![github-last-commit](https://img.shields.io/github/last-commit/nebius/soperator)](#)
<br/>
[![github-license](https://img.shields.io/github/license/nebius/soperator)](#-license)

[//]: # (Short description)
Run Slurm in Kubernetes and enjoy the benefits of both systems.

![Slurm in Kubernetes](docs/images/slurm_in_k8s_diagram.svg)



## 📋 Table of Contents
- [💡 Rationale](#-rationale)
- [⭐ Features](#-features)
- [❌ Limitations](#-limitations)
- [🚀 Installation](#-installation)
- [📈 Future Plans](#-future-plans)
- [📚 Documentation](#-documentation)
- [🤬 Feedback](#-feedback)
- [🤝 Contribution](#-contribution)
- [🏛 License](#-license)



## 💡 Rationale
Both [Slurm](https://slurm.schedmd.com/overview.html) and [Kubernetes](https://kubernetes.io/docs/concepts/overview/)
can be used in the role of workload manager for distributed model training and High-Performance Computing (HPC) in
general.

Each of these systems has its strengths and weaknesses, and the trade-offs are significant. Slurm boasts advanced and
effective scheduling, granular hardware control, and accounting, though lacks universality. On the other hand,
Kubernetes can be used for purposes other than training (e.g. inference), has good auto-scaling and self-healing.

[//]: # (TODO: Refer to the Slurm VS Kubernetes blog post)

It's unfortunate that there is no way to leverage the advantages of both solutions. In addition, some ML engineers
working in Big Tech don't even have a choice, as Kubernetes is the default infrastructure layer in their companies, and
no one supports a separate system for model training.

That's why we decided to marry these systems following the "Kubernetes-first" approach. We implemented a [Kubernetes
operator](https://kubernetes.io/docs/concepts/extend-kubernetes/operator/) - a piece of software that runs and manages
Slurm clusters represented via Kubernetes resources.

![Solution Architecture](docs/images/architecture_diagram.svg)

This allowed us to reuse Kubernetes' autoscaling and self-healing in Slurm, and implement several unique features, while
maintaining the usual way of interacting with it.

[//]: # (TODO: Refer to the Soperator blog post)



## ⭐ Features


### Shared Root Filesystem
When users interact with a Slurm cluster they see a shared file system as their root "**/**" directory. This approach
allowed us to retain the familiar way of using Slurm (e.g. users don't have to run all jobs in containers).

It also frees users from maintaining nodes in an identical state. They can, on one node, install new software packages,
create new Linux users, write job outputs, or download datasets, and **instantly get the changes on all other nodes**.


### GPU Health Checks
This is only applicable to NVIDIA GPUs.

The operator performs GPU health checks periodically. If any Slurm node shows an unsatisfactory result, the operator
“drains” it, which excludes the node from scheduling new jobs on it.


### Easy Scaling
The production of ML products often involves several stages each of which requires different computing power.

This solution allows Slurm to reuse the unique Kubernetes' ability to scale automatically depending on the current
needs. You can simply change a single value in the YAML manifest, and watch the cluster changes in size.


### High Availability
Kubernetes brings some level of HA out of the box. If a Pod or container dies (e.g. Slurm controller), Kubernetes
recreates it.

Our operator improves this further, continuously bringing the entire cluster to the desired state.


### Isolation of User Actions
Users can’t unintentionally break the Slurm cluster itself - all their actions are isolated within a dedicated
environment (some sort of container). This clearly defines the boundary between the operator's responsibility and the
users' one.





## ❌ Limitations
- **GPUs are required**. Although supporting CPU-only clusters or partitions seems pretty straightforward, we haven’t
  done it yet.
- **Scaling clusters down**. Only scaling up works flawlessly. Scaling down remains deleted nodes in the controller
  view. However, they can be removed manually using `scontrol`.
- **Single-partition cluster**. The Slurm's ability to split clusters into several partitions isn't supported now.
- **Software versions**. The list of software versions we currently support is quite short.
    - Linux distribution: Ubuntu [20.04](https://releases.ubuntu.com/focal/) and [22.04
      ](https://releases.ubuntu.com/jammy/).
    - Slurm: versions `23.11.6` and `24.05.3`.
    - CUDA: version [12.2.2](https://developer.nvidia.com/cuda-12-2-2-download-archive).
    - Kubernetes: >= [1.28](https://kubernetes.io/blog/2023/08/15/kubernetes-v1-28-release/).
    - Versions of some preinstalled software packages can't be changed.



## 🚀 Installation
The steps that need to be done to deploy Soperator to your Kubernetes cluster depend on whether you use an on-premise
K8s or some kind of cloud solution.


### Nebius Cloud
For [Nebius Cloud](https://nebius.ai/), we provide a Terraform recipe that creates everything itself, which includes:
- [Managed Kubernetes](https://nebius.ai/services/managed-kubernetes) cluster.
- [Virtual network](https://nebius.ai/services/vpc) and public IP addresses.
- At least one shared [File storage](https://nebius.ai/docs/compute/concepts/filesystem) where your environment is kept.
  It's implemented as a distributed filesystem focused on concurrent reading and writing.

Everything specific to Nebius Cloud is contained in a separate repository:
[nebius/soperator-terraform](https://github.com/nebius/soperator-terraform).

[//]: # (TODO: Change repo in the link when it's moved to Nebius SA library)


### Other Clouds
We don't provide terraform recipes for other clouds at the moment. However, you can implement them by analogy with the
Nebius one.

That is, to install in other clouds, use the instructions for on-premise Kubernetes.


### On-Premise Kubernetes
> [!IMPORTANT]
> When using the soperator, it is important that the CNI supports preserving the client source IP. 
> Therefore, if kube-proxy is configured in IPVS mode, or if you're using CNI-plugins like kube-router or Antrea Proxy, the operator will not work. 
> This operator has been tested with [Cilium network plugin](https://kubernetes.io/docs/concepts/extend-kubernetes/compute-storage-net/network-plugins/) 
> running in [kube-proxy replacement mode](https://docs.cilium.io/en/stable/network/kubernetes/kubeproxy-free/#kubernetes-without-kube-proxy).

In general, you need to follow these steps:
1. Decide on the shared storage technology you would like to use. At least one shared file system is necessary, because
   it will store that very environment shared among all Slurm nodes. The only thing the Soperator will require of you is
   the [PVC](https://kubernetes.io/docs/concepts/storage/persistent-volumes/) name. Consider using [NFS
   ](https://kubernetes.io/docs/concepts/storage/volumes/#nfs) as the simplest option, or something more advanced
   like [OpenEBS](https://openebs.io/) or [GlusterFS](https://www.gluster.org/).
2. Install the [NVIDIA GPU operator](https://github.com/NVIDIA/gpu-operator).
3. If you use InfiniBand, install the [NVIDIA Network operator](https://github.com/Mellanox/network-operator).
4. Install Soperator by applying the [slurm-operator](helm/slurm-operator) Helm chart.
5. Create a Slurm cluster by applying the [slurm-cluster](helm/slurm-cluster) Helm chart.
6. Wait until the `slurm.nebius.ai/SlurmCluster` resource becomes `Available`.

[//]: # (TODO: Refer to Helm OCI images instead of file directories when the repo is open)

> [!WARNING]
> Although Soperator doesn't contain fundamental incompatibilities with any Kubernetes installation, we haven't tested
> it anywhere outside Nebius, so it's likely that something won't work out of the box or will require additional
> configuration. Contact us on a GitHub issue, and we will help you to install Soperator to your Kubernetes and update
> these docs to make it more coherent.



## 📈 Future Plans
- 🛠 **Slurm accounting**. We're working on bringing it to this solution all the advantages of Slurm accounting.
- 🛠 **CPU-only clusters**. Some Slurm users don't need GPU computations, so we are working on supporting CPU-only
  clusters.
- 💡 **On-demand nodes**. The easy scaling can be improved further by provisioning new K8s nodes only when there are
  queued jobs that need them.
- 💡 **Network topology-aware job scheduling**. Thanks to the Slurm topology feature, we can support detailed
  configuration of the network topology to make scheduling more beneficial.
- 💡 **Automatic replacement of bad-performing nodes**. Now the operator only drains Slurm nodes that do bad. We have a
  plan to replace such nodes automatically.
- 💡 **More system checks**. Soperator only checks GPUs at the moment, but there are more things to check: software
  issues, storage performance, network connectivity, etc. So we're going to continue adding new checks.
- 💡 **Jail backups**. This implies backing up the shared storage to improve durability.
- 💡 **Automatic external checkpointing**. We consider using NVIDIA's [cuda-checkpoint
  ](https://github.com/NVIDIA/cuda-checkpoint) for dumping and resuming job processes externally.



## 📚 Documentation
The detailed documentation is located in the [docs](docs) directory of this repository.

It includes, among other things:
- A detailed description of the Soperator [architecture](docs/architecture.md).
- The [full list of features](docs/features.md) that this solution provides comparing to typical Slurm installations.
- A more complete description of the existing [limitations](docs/limitations.md).



## 🤬 Feedback
If you like this project, **star in on GitHub**. So we will see that the community is interested in it and continue
developing it further, openly and publicly.

If you failed to install Soperator to your Kubernetes cluster or encounter any other issue with it, create a [GitHub
issue](https://github.com/nebius/soperator/issues) and write details about your problem. We will try to help.

> [!NOTE]
> This project is very new and quite raw - it was started in May 2024. And if it already works stably in Nebius, this
> may not be the case for other clouds.



## 🤝 Contribution
Unfortunately, at the moment we don't have development docs for outside developers who want to participate in this
project. If you are interested in contribution, create a GitHub issue, and we'll figure something out.

Also, pay attention to the list of future plans we have. The tasks we are currently working on are marked there. Maybe
you need just one of these.



## 🏛 License
The Soperator iself is licensed under [Apache 2.0](LICENSE) - a permissive free software license that allows you to use
the software for any purpose, to distribute it, to modify it, and to distribute modified versions under specific terms.

Please note that various pieces of software it installs in your cluster may have other licenses.
