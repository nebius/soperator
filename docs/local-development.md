# Local Development with Kind

This guide describes how to set up a local Kubernetes cluster using Kind (Kubernetes in Docker) for development and testing of soperator.

## Prerequisites

The Makefile includes automated installation of required tools:
- **kind** - Kubernetes in Docker
- **flux** - Flux CD CLI
- **helm** - Helm package manager

All tools will be installed to `./bin/` directory automatically when needed.

## Quick Start

### 1. Install Tools

Install kind and flux CLI:

```bash
make kind
make flux
```

### 2. Create Kind Cluster

Create a local Kubernetes cluster with 5 nodes (1 control-plane + 4 workers):

```bash
make kind-create
```

This will create a cluster named `soperator-dev` with 5 nodes by default and automatically switch your kubectl context to `kind-soperator-dev`.

### 3. Verify Cluster

Check cluster status:

```bash
kubectl cluster-info
kubectl get nodes
```

You should see 5 nodes (1 control-plane + 4 workers).

## Customization

### Custom Number of Nodes

Create a cluster with a different number of nodes:

```bash
make kind-create KIND_NODES=3
```

### Custom Cluster Name

Use a different cluster name:

```bash
make kind-create KIND_CLUSTER_NAME=my-cluster KIND_NODES=5
```

## Available Commands

| Command | Description |
|---------|-------------|
| `make install-kind` | Install kind CLI to ./bin/ |
| `make install-flux` | Install flux CLI to ./bin/ |
| `make kind-create` | Create kind cluster with specified nodes (auto-switches kubectl context) |
| `make kind-delete` | Delete kind cluster |
| `make kind-list` | List all kind clusters |
| `make kind-restart` | Restart cluster (delete + create) |
| `make kind-status` | Check kind cluster status and deployments |
| `make deploy-flux` | Deploy soperator via Flux CD |
| `make undeploy-flux` | Remove Flux CD configuration |
| `make jail-shell` | Open interactive shell in jail environment via login pod |

> **Note:** The `kind-create` command automatically switches your kubectl context to the newly created cluster, so you can immediately start using `kubectl` commands without manually switching contexts.

## Working with the Cluster

### Deploy Operator

Deploy soperator to the kind cluster using Flux CD:

```bash
# Deploy via Flux CD (recommended for local development)
make deploy-flux

# This will:
# 1. Install Flux CD
# 2. Deploy soperator-fluxcd configuration
# 3. Set up minimal local environment
```

Alternative: Deploy directly without Flux:

```bash
# Install CRDs
make install

# Deploy operator directly
make deploy
```

### Install with Helm

```bash
# Install soperator with Helm
helm install soperator ./helm/soperator -n soperator-system --create-namespace
```

### Working with Flux CD

Monitor Flux CD deployments:

```bash
# Check all Flux resources
flux get all -n flux-system

# Check HelmReleases
kubectl get helmreleases -n flux-system

# Check HelmRepositories
kubectl get helmrepositories -n flux-system

# Watch reconciliation
flux logs --follow

# Force reconciliation
flux reconcile helmrelease soperator-fluxcd -n flux-system
```

Customize local deployment:

```bash
# Edit values in the ConfigMap
kubectl edit configmap soperator-fluxcd-values -n flux-system

# Trigger reconciliation after changes
flux reconcile helmrelease soperator-fluxcd -n flux-system
```

### Interactive Shell in Jail Environment

Once the Slurm cluster is deployed, you can access an interactive shell inside the jail environment (shared root filesystem) via the login pod:

```bash
make jail-shell
```

This command opens a bash shell inside the jail environment where you can:
- Test installed software and packages
- Verify user environments
- Debug job execution issues
- Inspect shared filesystem state
- Run commands as if you're on a Slurm login node

The jail environment is the shared root filesystem (`/`) that all Slurm nodes see, making this useful for:
- Installing additional software packages
- Creating test users
- Debugging PATH and environment variables
- Verifying shared data availability

## Cleanup

Delete the kind cluster when done:

```bash
make kind-delete
```

## Platform Support

The Makefile automatically detects your platform and installs the correct binaries:

- **macOS**: Supports both Intel (amd64) and Apple Silicon (arm64)
- **Linux**: Supports amd64 and arm64 architectures

## Troubleshooting

### Cluster Already Exists

If you see "Cluster already exists" error:

```bash
make kind-delete
make kind-create
```

Or use the restart command:

```bash
make kind-restart
```

### Docker Not Running

Ensure Docker Desktop (macOS) or Docker daemon (Linux) is running:

```bash
docker ps
```

### kubectl Context

The `make kind-create` command automatically switches kubectl context to the created cluster. 

To manually switch context if needed:

```bash
kubectl config use-context kind-soperator-dev
```

To view current context:

```bash
kubectl config current-context
```

To view all available contexts:

```bash
kubectl config get-contexts
```

## Advanced Configuration

For custom kind cluster configuration, you can modify the generated config:

```yaml
kind: Cluster
apiVersion: kind.x-k8s.io/v1alpha4
nodes:
- role: control-plane
  extraPortMappings:
  - containerPort: 30022
    hostPort: 30022
    protocol: TCP
- role: worker
- role: worker
- role: worker
- role: worker
```

## Integration with CI/CD

The kind setup can be used in CI/CD pipelines for automated testing:

```bash
# In CI script
make kind-create KIND_NODES=3
make install
make deploy
# Run tests
make test
make kind-delete
```
