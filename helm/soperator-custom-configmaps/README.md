# soperator-custom-configmaps

This Helm chart deploys custom ConfigMaps for the Soperator deployment.

## Structure

The chart contains configuration files that are deployed as Kubernetes ConfigMaps:

- **supervisord.conf** - Supervisord configuration for managing slurmd, sshd, and dockerd processes
- **daemon.json** - Docker daemon configuration with NVIDIA runtime support
- **enroot.conf** - Enroot container configuration paths
- **95-nebius-o11y** - MOTD (Message of the Day) script for Nebius observability

## Configuration Files Location

All configuration files are stored in the `config-files/` directory and are automatically included in their respective ConfigMaps during deployment.

## Installation

```bash
helm install soperator-custom-configmaps ./helm/soperator-custom-configmaps \
  --namespace soperator \
  --create-namespace
```

## Configuration

You can customize the deployment by modifying `values.yaml`:

```yaml
# Namespace where ConfigMaps will be created
namespace: soperator

# Enable/disable individual ConfigMaps
configMaps:
  supervisord:
    enabled: true
  motd:
    enabled: true
  imageStorage:
    enabled: true
```

## Values

| Parameter | Description | Default |
|-----------|-------------|---------|
| `namespace` | Namespace where ConfigMaps will be created | `soperator` |
| `configMaps.supervisord.enabled` | Enable supervisord ConfigMap | `true` |
| `configMaps.supervisord.name` | Name of supervisord ConfigMap | `custom-supervisord-config` |
| `configMaps.motd.enabled` | Enable MOTD ConfigMap | `true` |
| `configMaps.motd.name` | Name of MOTD ConfigMap | `motd-nebius-o11y` |
| `configMaps.imageStorage.enabled` | Enable image storage ConfigMap | `true` |
| `configMaps.imageStorage.name` | Name of image storage ConfigMap | `image-storage` |

## Deployed ConfigMaps

This chart creates the following ConfigMaps:

1. **custom-supervisord-config** - Contains supervisord.conf
2. **motd-nebius-o11y** - Contains 95-nebius-o11y script
3. **image-storage** - Contains daemon.json and enroot.conf
