<div align="center">

# Soperator

### Run Slurm on Kubernetes. Anywhere.

The open-source Kubernetes operator that lets platform and DevOps teams stand up
production-grade Slurm clusters for AI training and HPC — on any cloud, any infrastructure.

**Fast setup & day-2 ops · Fault-tolerant training · High GPU utilization**

_Running in production on Nebius today, orchestrating distributed training across thousands of GPUs._
_Apache 2.0. Free forever. Same code in every install — no paid-tier fork._

<br/>

[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)
[![Release](https://img.shields.io/github/v/release/nebius/soperator)](https://github.com/nebius/soperator/releases)
[![Go Report](https://goreportcard.com/badge/github.com/nebius/soperator)](https://goreportcard.com/report/github.com/nebius/soperator)
[![Stars](https://img.shields.io/github/stars/nebius/soperator?style=social)](https://github.com/nebius/soperator/stargazers)
[![Discussions](https://img.shields.io/github/discussions/nebius/soperator)](https://github.com/nebius/soperator/discussions)
[![Issues](https://img.shields.io/github/issues/nebius/soperator)](https://github.com/nebius/soperator/issues)

[📚 Docs](https://docs.nebius.com/slurm-soperator/) · [🗒️ Releases](https://github.com/nebius/soperator/releases) · [💬 Discussions](https://github.com/nebius/soperator/discussions) · [🐞 Issues](https://github.com/nebius/soperator/issues)

<br/>

![Soperator overview](docs/images/layers_diagram.png)

</div>

---

## 📋 Table of contents

- [What is Soperator](#-what-is-soperator)
- [Why Soperator](#-why-soperator)
- [Key features](#-key-features)
- [How it works](#-how-it-works)
- [What's new](#-whats-new)
- [Roadmap](#-roadmap)
- [Deployment options](#-deployment-options)
- [Requirements & supported versions](#-requirements--supported-versions)
- [Documentation](#-documentation)
- [Community & contributing](#-community--contributing)
- [License](#-license)

---

## ❓ What is Soperator

Slurm is the standard scheduler for AI training and HPC. Kubernetes is the de facto platform for modern infrastructure. **Soperator is the bridge** — a Kubernetes operator that reconciles a declarative `SlurmCluster` Custom Resource into a fully functional Slurm cluster, with the drivers, the CUDA/NCCL stack, the shared filesystem, the health checks and the accounting already wired up.

It's built for platform and DevOps engineers who need to deliver Slurm to researchers **without becoming full-time Slurm operators themselves**, and for anyone migrating off bare-metal Slurm, standing up managed Slurm on their cloud, or running AI training clusters at scale.

## 🎯 Why Soperator

Slurm is hard to operate well. Soperator takes on the three problems that consume platform teams the most.

| Problem | How Soperator solves it |
|---|---|
| **Slow cluster setup and painful day-2 ops.** Deploying Slurm takes weeks of glue code. Resizing, upgrading or reconfiguring a live cluster is just as painful. Keeping the software stack identical on every node is a constant challenge. | Declarative cluster management through a single `SlurmCluster` CR, plus a shared root filesystem (the *jail*), reduce setup time and eliminate configuration drift across nodes. |
| **Fragile training on flaky hardware.** One bad GPU can kill a multi-day run. Teams end up manually watching clusters 24/7. | Passive and active health checks detect GPU, network, storage and system issues; failed nodes are drained and replaced automatically; the control plane self-heals after failures and restarts. |
| **Poor GPU utilization.** Static clusters leave GPUs idle. Naive scheduling wrecks collective throughput. | Ephemeral workers, InfiniBand-topology-aware placement, and full native Slurm scheduling — so you get more throughput per GPU-hour without overprovisioning. |

---

## ⭐ Key features

Organized around the three problems above. Everything listed here is in the codebase today.

### Fast cluster setup and day-2 ops

- **Kubernetes-native operator.** Describe the whole cluster — controllers, login nodes, workers, accounting, storage — as a single `SlurmCluster` CR. The operator continuously reconciles it to match the spec.
- **Jail (shared root filesystem).** Every node sees one unified filesystem. Install a package or edit a config once and it's live on every login and worker node — no containerized jobs required, no per-node image drift.
- **Pre-installed AI/ML stack.** NVIDIA drivers, CUDA, NCCL, `nccl-tests` and common training dependencies are baked into the images, with an explicit CUDA ↔ NCCL version mapping — no compatibility-matrix hunt.
- **Declarative day-2 management.** Upgrades, resizing, `NodeSet` changes and configuration edits are all driven by edits to `SlurmCluster` — no manual steps on the nodes.
- **Identity and accounting built in.** SSSD for centralized users and groups (LDAP / AD / FreeIPA), Tailscale for secure SSH over a Tailnet, and native Slurm accounting for per-job / per-user / per-account metrics.
- **Native observability.** First-class integrations with Prometheus, Grafana and Loki for metrics, dashboards and logs — no custom exporters to glue in.

### Fault-tolerant training

- **Passive health checks.** Continuous monitoring of Kubernetes and Slurm control-plane signals, plus node-local conditions including NVMe disk health.
- **Active health checks.** `ActiveCheck` resources run scheduled GPU, system, storage and network probes — including GPU performance checks that catch slow or flaky GPUs before they silently drag down a distributed job.
- **Automatic draining, replacement and self-healing.** Failed nodes are drained and replaced automatically; Kubernetes keeps controllers, the accounting database and login nodes reconciled to the declared state after failures, restarts and rolling updates.

### High GPU utilization

- **Ephemeral nodes and autoscaling.** Workers are provisioned on demand and scaled down when the queue drains — Slurm is no longer locked to a fixed fleet size.
- **InfiniBand topology awareness.** Correct InfiniBand topology for GPU nodes, tier-2 switch constraints, and automatic exclusion of CPU-only nodes from the InfiniBand tree — so `sbatch` placement reflects real network distance.
- **Container runtime support.** Pyxis / Enroot and OCI-compatible runtimes for jobs that still want image-based isolation.
- **Full Slurm scheduler semantics intact.** Gang scheduling, fair-share, preemption, reservations and dependencies all work as researchers expect.

---

## 💡 How it works

Soperator is a standard Kubernetes operator pattern applied to Slurm.

1. **You declare a `SlurmCluster`.** A single Custom Resource describes the entire cluster topology — controllers, login nodes, worker `NodeSet`s, accounting DB, shared volumes, health checks, observability, identity integration.
2. **The operator reconciles.** Soperator's controllers translate the spec into the concrete Kubernetes objects — Deployments, StatefulSets, PVCs, Services, ConfigMaps, Slurm configuration — and keep them in sync with the spec as the cluster runs.
3. **The jail becomes the cluster's root filesystem.** A shared PVC is mounted into every login and worker node as its root, so one change to a binary, library or config file is instantly visible cluster-wide. Users get a familiar Linux environment, not a "build a container for every job" workflow.
4. **Health checks run continuously.** Passive checks watch control-plane and node signals; `ActiveCheck` resources run scheduled probes against GPUs, networking, storage and the system. Failed nodes are automatically drained and replaced.
5. **Slurm stays Slurm.** `sbatch`, `srun`, `sinfo`, accounting, reservations, dependencies and preemption behave exactly as researchers already expect — Soperator does not fork or rewrite the scheduler.

![Soperator architecture](docs/images/architecture_diagram.svg)

**Design tenets**

- **Declarative and GitOps-friendly.** Everything the cluster does is reconciled from the `SlurmCluster` spec.
- **Portable.** AWS, GCP, Azure, Nebius, OCI, bare-metal, air-gapped — if you have a conformant Kubernetes cluster with GPUs, you have Soperator.
- **Cloud-native integrations you already run.** Helm, Prometheus, Grafana, Loki, Argo/Flux, cert-manager, Cilium, NVIDIA GPU Operator — native, not bolted on.
- **Same code, everywhere.** The Apache-2.0 codebase is what runs in self-deploy, managed and pro installs alike. No enterprise fork.

📰 Deeper reading: [engineering deep-dive on Medium](https://medium.com/nebius) · [`docs/architecture.md`](docs/architecture.md)

---

## 🗒️ What's new

Highlights from recent releases. Full log on the [releases page](https://github.com/nebius/soperator/releases).

1. **3.0.3 — Passive health and probe controls.** Passive health check for NVMe disks, tunable liveness and readiness probe templates for all Soperator CRDs, and fixes to the `ActiveCheck` job controller.
2. **3.0.2 — Slurm 25.11.3 and dependency refresh.** Upgraded to Slurm 25.11.3, added an explicit CUDA ↔ `nccl-tests` version mapping, migrated off deprecated container registries, and fixed Enroot cleanup.
3. **3.0.1 — InfiniBand topology.** Correct InfiniBand topology for GPU nodes, the ability to add tier-2 topology switches as constraints, and filtering of CPU-only nodes from the InfiniBand tree.
4. **3.0.0 — Ephemeral nodes and operator hardening.** Introduced ephemeral nodes for elastic worker pools, Slurm 25.11.2, parallel image builds for jail and controllers, Helm retry configuration for all releases, and removal of the legacy dynamic-workers path.
5. **2.0.x — Reliability fixes.** Long-terminating worker pod fix and an NVIDIA container toolkit upgrade to 1.18.2-1.

---

## 📈 Roadmap

Some milestones we're heading toward in 2026. Track progress in [Issues](https://github.com/nebius/soperator/issues) and [Discussions](https://github.com/nebius/soperator/discussions).

- **Smarter health checks.** Continued improvements to active and passive checks for earlier detection and better resilience on long-running jobs.
- **Automatic acceptance tests.** Faster validation of new cluster configurations with less manual verification.
- **Next-gen GPU platforms.** Support for GB300-based systems so customers can run the latest large-scale training workloads.
- **Local disk support.** High-speed node-local storage for performance-sensitive training, faster checkpointing and efficient data staging.
- **NCCL profiling dashboards.** First-class visibility into collective communication to spot bottlenecks and tune multi-node training.
- **Automatic capacity sharing between training and inference.** Shift capacity as demand changes, without a second cluster.
- **Multi-cluster, multi-cloud scheduling.** Coordinate workloads across environments and operate beyond a single cluster boundary.

---

## 🚀 Deployment options

Four paths, same codebase. Pick the one that matches how much of the stack you want to own.

| Path | Best for |
|---|---|
| **Self-deploy on any Kubernetes** | Teams running their own K8s, on any cloud or on-prem. [Learn more](https://docs.nebius.com/slurm-soperator/deploy/overview#self-deployment-on-other-platforms-and-on-premises). |
| **Self-deploy on Nebius (Terraform)** | Teams standing up a greenfield cluster on Nebius. [Learn more](https://docs.nebius.com/slurm-soperator/deploy/overview#self-deployment-in-nebius-ai-cloud). |
| **Managed Soperator on Nebius** | Teams who want a training-ready cluster and only want to pay for compute. [Learn more](https://nebius.com/services/soperator). |
| **Soperator Pro on Nebius** | Teams who want Nebius engineers to install, tune and support it for their workload. [Learn more](https://docs.nebius.com/slurm-soperator/deploy/overview#pro-solution-for-soperator). |

---

## 🧪 Requirements & supported versions

| Component | Version |
|---|---|
| Linux (node images) | Ubuntu 24.04 |
| Slurm | 25.11.3 |
| CUDA | 12.9 |
| NCCL | aligned with CUDA (see [`docs/versions.md`](docs/versions.md)) |
| Kubernetes | ≥ 1.31 |
| Helm | ≥ 3.14 |
| NVIDIA GPU Operator | latest stable |
| CNI | Cilium (kube-proxy replacement) recommended |

Some pre-installed software versions are pinned to the images Soperator ships. See [`docs/limitations.md`](docs/limitations.md) for current caveats, including the single-partition and GPU-only or CPU-only cluster constraints.

---

## 📚 Documentation

The full documentation is published at [docs.nebius.com/slurm-soperator](https://docs.nebius.com/slurm-soperator/), with the source in the [`docs/`](docs/) directory of this repository. It covers, among other things:

- A detailed description of the Soperator architecture.
- The full list of features this solution provides compared to typical Slurm installations.
- A more complete description of the existing limitations.
- Local development with Kind — setting up a local Kubernetes cluster for development and testing.
- The release process for both the `soperator` and `nebius-solutions-library` repositories.
- Metrics collection and processing.
- Logs collection and aggregation pipeline.

---

## 🤝 Community & contributing

Soperator is built in the open. Platform and DevOps engineers running Slurm on Kubernetes — anywhere — should feel at home here.

- ⭐ **Star** the repo if Soperator is useful to you.
- 🐞 **Report bugs and request features** in [Issues](https://github.com/nebius/soperator/issues).
- 💬 **Ask questions and share patterns** in [Discussions](https://github.com/nebius/soperator/discussions).
- 🛠️ **Contribute code** — see [`CONTRIBUTING.md`](CONTRIBUTING.md). Good first issues are labeled [`good first issue`](https://github.com/nebius/soperator/issues?q=is%3Aissue+is%3Aopen+label%3A%22good+first+issue%22).
- 🔒 **Security** — report vulnerabilities per [`SECURITY.md`](SECURITY.md).
- 📰 **Blog** — [Introducing Soperator](https://nebius.com/blog/posts/soperator) · [Managed Soperator launch](https://nebius.com/blog/posts/managed-soperator) · [Soperator explained](https://nebius.com/blog/posts/soperator-explained).

We follow the [Contributor Covenant Code of Conduct](CODE_OF_CONDUCT.md).

---

## 🏛 License

Soperator is licensed under [Apache 2.0](LICENSE). Software it installs into your cluster may carry other licenses; please review for your use case.

---

<div align="center">

**Built by [Nebius](https://nebius.com) and the Soperator community.**
_Run Slurm on Kubernetes. Anywhere._

</div>
