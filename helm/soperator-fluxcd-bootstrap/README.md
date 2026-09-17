# soperator-fluxcd-bootstrap

Bootstrap Helm chart for installing soperator-fluxcd via FluxCD.

## Description

This umbrella chart deploys the necessary FluxCD resources to bootstrap the soperator-fluxcd deployment:
- **HelmRepository**: Configures the OCI registry source for soperator charts
- **HelmRelease**: Manages the installation and lifecycle of the soperator-fluxcd chart

## Installation

```bash
helm install soperator-bootstrap ./helm/soperator-fluxcd-bootstrap \
  --namespace flux-system \
  --create-namespace
```

## Configuration

### HelmRepository Settings

| Parameter | Description | Default |
|-----------|-------------|---------|
| `helmRepository.enabled` | Enable HelmRepository creation | `true` |
| `helmRepository.name` | Name of the HelmRepository | `soperator-fluxcd` |
| `helmRepository.namespace` | Namespace for HelmRepository | `flux-system` |
| `helmRepository.interval` | Sync interval for repository | `5m` |
| `helmRepository.type` | Repository type (oci, default) | `oci` |
| `helmRepository.url` | OCI registry URL | `oci://cr.eu-north1.nebius.cloud/soperator` |

### HelmRelease Settings

| Parameter | Description | Default |
|-----------|-------------|---------|
| `helmRelease.enabled` | Enable HelmRelease creation | `true` |
| `helmRelease.name` | Name of the HelmRelease | `soperator-fluxcd` |
| `helmRelease.namespace` | Namespace for HelmRelease | `flux-system` |
| `helmRelease.interval` | Reconciliation interval | `5m` |
| `helmRelease.timeout` | Timeout for operations | `5m` |
| `helmRelease.chart.name` | Chart name to install | `helm-soperator-fluxcd` |
| `helmRelease.chart.version` | Chart version | `1.23.0` |
| `helmRelease.chart.interval` | Chart fetch interval | `5m` |

### Install/Upgrade Settings

| Parameter | Description | Default |
|-----------|-------------|---------|
| `helmRelease.install` | Install configuration (map) | See values.yaml |
| `helmRelease.upgrade` | Upgrade configuration (map) | See values.yaml |
| `helmRelease.valuesFrom` | Array of ConfigMap references for values | See values.yaml |

## Example: Custom Values

```yaml
helmRepository:
  url: oci://custom-registry.example.com/soperator
  interval: 10m

helmRelease:
  chart:
    version: "1.24.0"
  interval: 10m
  timeout: 10m
  install:
    createNamespace: true
    crds: Skip
    remediation:
      retries: 5
```

## Example: Using with ConfigMaps

The chart expects values to be provided via ConfigMaps:

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: soperator-fluxcd-values
  namespace: flux-system
data:
  values.yaml: |
    soperator:
      enabled: true
    observability:
      enabled: true
```

## Version Sync

The chart version is automatically synchronized via `make sync-version` from the main VERSION file.
