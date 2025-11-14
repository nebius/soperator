# Kind Cluster Examples

## Example 1: Basic Development Setup

```bash
# Install tools
make kind
make flux

# Create 5-node cluster (automatically switches kubectl context)
make kind-create

# Verify cluster
kubectl get nodes

# Deploy soperator via Flux CD
make deploy-flux

# Check deployment status
flux get all -n flux-system
kubectl get pods -n soperator-system
```

## Example 2: Custom Configuration

```bash
# Create cluster with 3 nodes (automatically switches context)
make kind-create KIND_NODES=3 KIND_CLUSTER_NAME=test-cluster

# Verify current context
kubectl config current-context
# Output: kind-test-cluster

# Use kubectl (no need to specify context - already switched)
kubectl get nodes

# Clean up
make kind-delete KIND_CLUSTER_NAME=test-cluster
```

## Example 3: Testing Local Images

```bash
# Create cluster
make kind-create

# Build operator image
docker build -t cr.eu-north1.nebius.cloud/soperator/slurm-operator:dev .

# Load image into kind
make kind-load-images

# Deploy with custom image
helm install soperator ./helm/soperator \
  --set controllerManager.manager.image.tag=dev \
  -n soperator-system --create-namespace
```

## Example 4: CI/CD Pipeline

```bash
#!/bin/bash
set -e

# Setup
make kind-create KIND_NODES=3

# Install CRDs
make install

# Run tests
make test
make helmtest

# Cleanup
make kind-delete
```

## Example 5: Flux CD Local Development

```bash
# Create cluster
make kind-create

# Deploy via Flux CD
make deploy-flux

# Monitor Flux reconciliation
flux logs --follow

# Check HelmRelease status
kubectl get helmreleases -n flux-system -w

# Customize configuration by editing ConfigMap
kubectl edit configmap soperator-fluxcd-values -n flux-system

# Force reconciliation after changes
flux reconcile helmrelease soperator-fluxcd -n flux-system

# Check soperator pods
kubectl get pods -n soperator-system

# Undeploy when done
make undeploy-flux
```

## Example 6: Flux CD with GitRepository (Advanced)

```bash
# Create cluster
make kind-create

# Install Flux
./bin/flux install

# Bootstrap Flux (for testing)
./bin/flux bootstrap github \
  --owner=<your-org> \
  --repository=<your-repo> \
  --path=clusters/dev \
  --personal

# Deploy soperator via Flux
kubectl apply -f helm/soperator-fluxcd/
```

## Example 7: Multi-node Worker Testing

```bash
# Create large cluster for testing worker distribution
make kind-create KIND_NODES=10

# Label nodes for different workloads
kubectl label nodes kind-soperator-dev-worker gpu=true
kubectl label nodes kind-soperator-dev-worker2 gpu=true
kubectl label nodes kind-soperator-dev-worker3 cpu-only=true

# Deploy slurm cluster with node filters
helm install slurm-cluster ./helm/slurm-cluster \
  -f your-custom-values.yaml \
  -n slurm --create-namespace
```
