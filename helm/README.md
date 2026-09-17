# Soperator Helm Charts

This directory contains all Helm charts for the Soperator project. Each chart serves a specific purpose in the overall Slurm cluster deployment and management.

## Chart Overview

### Core Operator Charts

#### `soperator/`
**Main Kubernetes operator for managing Slurm clusters**

The core controller that manages the lifecycle of Slurm clusters in Kubernetes. Handles CRDs (Custom Resource Definitions) for `SlurmCluster`, `NodeSet`, `TopologyConfig`, and other resources.

- Watches for Slurm-related CRs and reconciles them
- Manages cluster configuration and state
- Provides webhooks for validation and mutation
- Requires cert-manager for webhook certificates

**Dependencies**: cert-manager

#### `soperator-crds/`
**Custom Resource Definitions for Soperator**

Contains all CRDs used by the Soperator. Installed separately to allow independent lifecycle management of API definitions.

- `SlurmCluster` - defines Slurm cluster configuration
- `NodeSet` - defines compute node pools
- `TopologyConfig` - defines network topology
- Other supporting CRDs

**Note**: Should be installed before `soperator` chart.

#### `soperatorchecks/`
**Health check controller for Slurm resources**

Monitors and validates the health of Slurm cluster components. Performs periodic checks and updates resource statuses.

- Node health monitoring
- Slurm daemon status checks
- Resource availability verification
- Integration with observability stack

---

### Cluster Deployment Charts

#### `slurm-cluster/`
**Main Slurm cluster deployment**

Deploys a complete Slurm cluster with controller, database, and login nodes. This is the primary chart for creating a functional Slurm environment.

**Components**:
- `slurmctld` - Slurm controller daemon
- `slurmdbd` - Slurm database daemon (accounting)
- `slurmrestd` - REST API server
- Login nodes with SSH access
- Munge authentication service
- MariaDB for accounting (optional)

**Dependencies**: `slurm-cluster-storage`, munge keys, jail filesystem

#### `slurm-cluster-storage/`
**Persistent storage for Slurm cluster**

Provisions and manages persistent storage required by the Slurm cluster. Used in environments without a CSI controller but with an existing shared filesystem (e.g., pre-provisioned NFS, cloud file storage, or other shared storage solutions).

**Components**:
- Shared storage for `/home` directories
- Munge key storage
- Slurm configuration storage
- Accounting database storage
- NFS mounts configuration

**Note**: Must be installed before `slurm-cluster` chart.

#### `nodesets/`
**Compute node pools for Slurm**

Manages groups of compute nodes (worker nodes) for job execution. Allows dynamic scaling and heterogeneous node configurations.

**Features**:
- Multiple node pools with different specs
- GPU node support
- Custom resource limits per nodeset
- Integration with Kruise CloneSets for advanced deployment strategies
- Support for node taints and tolerations

---

### Infrastructure Charts

#### `nodeconfigurator/`
**Node configuration and reboot management**

Manages host-level configuration and controlled reboots of cluster nodes. Uses DaemonSets to ensure nodes are properly configured.

**Features**:
- Node reboot coordination
- OS-level configuration management
- Maintenance window support
- Safe reboot with workload draining

#### `nfs-server/`
**NFS server for shared storage**

Deploys an NFS server for providing shared filesystem access across the cluster.

**Use cases**:
- `/home` directory sharing
- Shared application data
- Configuration distribution
- Small to medium cluster storage needs

---

### Observability Charts

#### `soperator-activechecks/`
**Active health monitoring**

Performs active checks by submitting test jobs to verify cluster functionality.

**Checks**:
- Job submission and execution
- Scheduler responsiveness
- Node availability
- Resource allocation

#### `soperator-dcgm-exporter/`
**NVIDIA GPU metrics exporter**

Exports GPU metrics using NVIDIA DCGM (Data Center GPU Manager) for monitoring GPU utilization and health.

**Metrics**:
- GPU utilization
- Memory usage
- Temperature
- Power consumption
- ECC errors

**Dependencies**: NVIDIA GPU Operator

#### `soperator-notifier/`
**Alert notification service**

Sends notifications about cluster events and alerts to external services.

**Integrations**:
- Slack webhooks
- Email notifications
- Custom webhook endpoints

---

### Configuration Management Charts

#### `soperator-custom-configmaps/`
**Custom ConfigMaps for cluster nodes**

Deploys custom configuration files as Kubernetes ConfigMaps that are mounted into cluster nodes.

**Configurations**:
- `supervisord.conf` - Process management for slurmd, sshd, dockerd
- `daemon.json` - Docker daemon configuration with NVIDIA runtime
- `enroot.conf` - Enroot container runtime configuration
- `95-nebius-o11y` - MOTD (Message of the Day) for observability

**Use case**: Standardize node-level configurations across the cluster.

---

### FluxCD Integration Charts

#### `soperator-fluxcd/`
**Umbrella chart for FluxCD-managed deployment**

Comprehensive chart that defines HelmReleases for all Soperator components. Used with FluxCD for GitOps-style deployments.

**Components managed**:
- All operator charts
- Cluster deployment
- Observability stack
- Infrastructure components
- Dependencies (cert-manager, MariaDB operator, etc.)

**Features**:
- Centralized version management
- Environment-specific configurations
- Dependency ordering
- ConfigMap-based value overrides

#### `soperator-fluxcd-bootstrap/`
**Bootstrap chart for FluxCD setup**

Creates the initial HelmRepository and HelmRelease resources needed to deploy `soperator-fluxcd`.

**Purpose**: Simplifies the initial FluxCD setup by providing a single chart that bootstraps the entire deployment process.

**What it creates**:
- `HelmRepository` - points to OCI registry with Soperator charts
- `HelmRelease` - deploys `soperator-fluxcd` chart

**Recommended for**: Production deployments, GitOps workflows

---

## Chart Dependency Tree

```
soperator-fluxcd-bootstrap
  └── soperator-fluxcd (umbrella chart)
       ├── soperator-crds
       ├── soperator
       ├── soperatorchecks
       ├── nodeconfigurator
       ├── slurm-cluster-storage
       ├── slurm-cluster
       │    └── requires: slurm-cluster-storage
       ├── nodesets
       ├── soperator-activechecks
       ├── soperator-dcgm-exporter
       ├── soperator-notifier
       ├── soperator-custom-configmaps
       └── nfs-server
```

## Installation Order

### Manual Installation

1. **Prerequisites**
   ```bash
   # Install cert-manager
   helm install cert-manager jetstack/cert-manager \
     --namespace cert-manager \
     --create-namespace \
     --set installCRDs=true
   ```

2. **Core Operator**
   ```bash
   # Install CRDs
   helm install soperator-crds ./helm/soperator-crds \
     --namespace soperator-system \
     --create-namespace

   # Install operator
   helm install soperator ./helm/soperator \
     --namespace soperator-system
   ```

3. **Storage**
   ```bash
   helm install slurm-storage ./helm/slurm-cluster-storage \
     --namespace soperator
   ```

4. **Slurm Cluster**
   ```bash
   helm install slurm-cluster ./helm/slurm-cluster \
     --namespace soperator \
     -f my-values.yaml
   ```

### FluxCD Installation (Recommended)

```bash
# Install Flux
flux install

# Create values ConfigMap
kubectl create configmap soperator-fluxcd-values \
  -n flux-system \
  --from-file=values.yaml=my-values.yaml

# Bootstrap with Helm chart
helm install soperator-bootstrap \
  oci://cr.eu-north1.nebius.cloud/soperator/soperator-fluxcd-bootstrap \
  --version 1.23.0 \
  --namespace flux-system
```

## Version Management

All chart versions are synchronized automatically via `make sync-version` which reads from the `VERSION` file in the repository root.

To update versions:
```bash
# Update VERSION file
echo "1.24.0" > VERSION

# Sync all chart versions
make sync-version
```

## Development

### Local Testing

```bash
# Render templates
helm template test ./helm/soperator

# Dry-run installation
helm install test ./helm/soperator --dry-run

# Run unit tests
helm unittest ./helm/soperator
```

### Chart Packaging

```bash
# Package single chart
helm package ./helm/soperator -d ./helm-releases

# Package and push all charts
make release-helm
```

## Documentation

Each chart contains its own `README.md` with detailed configuration options. Refer to individual chart directories for specific documentation.

## Support

For issues and questions:
- GitHub Issues: https://github.com/nebius/soperator/issues
- Documentation: https://docs.nebius.com/slurm-soperator
