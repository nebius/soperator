# Local Environment Configuration

This directory contains the FluxCD configuration for local development with kind clusters.

## Overview

The local environment provides a minimal configuration suitable for:
- Local development and testing
- CI/CD pipelines
- Quick prototyping
- Feature validation

## Files

- **`kustomization.yaml`** - Main Kustomize configuration that ties everything together
- **`namespace.yaml`** - Creates the `flux-system` namespace
- **`helmrepository.yaml`** - Configures the OCI Helm repository for soperator charts
- **`helmrelease.yaml`** - Defines the HelmRelease for soperator-fluxcd
- **`values.yaml`** - Minimal configuration values for local development

## Configuration

### Default Settings

The local configuration has most optional components disabled:

- ✅ **Enabled**: soperator core operator
- ❌ **Disabled**: Slurm cluster, NodeSets, Active Checks
- ❌ **Disabled**: MariaDB Operator, Security Profiles Operator
- ❌ **Disabled**: Observability stack (VictoriaMetrics, Prometheus, DCGM)
- ❌ **Disabled**: Cert Manager, Backup, NFS Server, Notifier

### Customization

To enable additional components, edit `values.yaml`:

```yaml
# Example: Enable Slurm Cluster
slurmCluster:
  enabled: true
  namespace: soperator
```

Then apply changes:

```bash
# Rebuild and apply
kustomize build fluxcd/environment/local | kubectl apply -f -

# Or use make target
make deploy-flux
```

## Usage

### Quick Start

```bash
# Create kind cluster
make kind-create

# For unstable releases, sync version first
# (This updates the OCI registry URL based on version stability)
make sync-version

# Deploy with Flux
make deploy-flux
```

**Note**: The `make deploy-flux` command automatically detects whether the version is stable or unstable based on the `VERSION` file:
- **Stable versions** (e.g., `1.22.3`) use `oci://cr.eu-north1.nebius.cloud/soperator`
- **Unstable versions** (e.g., `1.22.3-fed4a485`) use `oci://cr.eu-north1.nebius.cloud/soperator-unstable`

To deploy an unstable/development version:
1. Set the unstable version in the `VERSION` file (e.g., `1.22.3-fed4a485`)
2. Run `make sync-version` to update all configuration files
3. Run `make deploy-flux` - it will automatically use the correct OCI registry

### Manual Deployment

```bash
# Install Flux
flux install

# Apply local configuration
kustomize build fluxcd/environment/local | kubectl apply -f -
```

### Monitoring

```bash
# Check Flux resources
flux get all -n flux-system

# Watch HelmRelease
kubectl get helmreleases -n flux-system -w

# View logs
flux logs --follow
```

### Cleanup

```bash
# Remove Flux configuration
make undeploy-flux

# Delete cluster
make kind-delete
```

## ConfigMap

The configuration uses a ConfigMap (`soperator-fluxcd-values`) to store values:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: soperator-fluxcd-values
  namespace: flux-system
data:
  values.yaml: |
    # Configuration content from values.yaml
```

This approach allows easy customization without rebuilding Kustomize manifests:

```bash
kubectl edit configmap soperator-fluxcd-values -n flux-system
flux reconcile helmrelease soperator-fluxcd -n flux-system
```

## HelmRepository

Points to the soperator OCI registry:

```yaml
spec:
  type: oci
  url: oci://cr.eu-north1.nebius.cloud/soperator
```

For development with unstable images, change to:

```yaml
spec:
  url: oci://cr.eu-north1.nebius.cloud/soperator-unstable
```

## Integration with CI/CD

Example GitHub Actions workflow:

```yaml
- name: Create kind cluster
  run: make kind-create

- name: Deploy soperator
  run: make deploy-flux

- name: Wait for deployment
  run: |
    kubectl wait --for=condition=ready helmrelease/soperator-fluxcd \
      -n flux-system --timeout=5m

- name: Run tests
  run: make test

- name: Cleanup
  run: make kind-delete
```

## Troubleshooting

### HelmRelease not reconciling

```bash
# Check HelmRelease status
kubectl describe helmrelease soperator-fluxcd -n flux-system

# Check source
kubectl describe helmrepository soperator-fluxcd -n flux-system

# Force reconciliation
flux reconcile source helm soperator-fluxcd -n flux-system
flux reconcile helmrelease soperator-fluxcd -n flux-system
```

### ConfigMap not found

```bash
# Verify ConfigMap exists
kubectl get configmap soperator-fluxcd-values -n flux-system

# Reapply if missing
kustomize build fluxcd/environment/local | kubectl apply -f -
```
