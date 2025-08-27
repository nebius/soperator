# NFS Server Helm Chart

A Helm chart for deploying an NFS server on Kubernetes with built-in monitoring capabilities.

## Features

- **StatefulSet**: Single instance NFS server with persistent storage
- **Storage Class**: Automatic NFS storage class creation for CSI driver
- **ConfigMap-based Exports**: NFS exports configuration managed by Helm templates
- **Multi-subnet Support**: Support for multiple client networks with individual export entries
- **Monitoring**: Optional NFS metrics collection with node_exporter

## Prerequisites

- Storage class for persistent volume (or use existing PVC)
- For CSI NFS provisioning: [NFS CSI Driver](https://github.com/kubernetes-csi/csi-driver-nfs)

## Configuration

### Core NFS Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `nfs.sharedDirectory` | Directory path to export | `/export` |
| `nfs.permitted` | List of allowed client networks (supports multiple subnets and wildcards) | `[10.0.0.0/8, 172.16.0.0/12, 192.168.0.0/16]` |
| `nfs.shareOptions` | NFS export options applied to all permitted networks | `rw,fsid=0,sync,no_subtree_check,no_auth_nlm,insecure,no_root_squash` |
| `nfs.graceTime` | NFS grace period (seconds) | `10` |
| `nfs.leaseTime` | NFS lease time (seconds) | `10` |
| `nfs.threads` | Number of NFS daemon threads | `8` |

### Storage Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `storage.size` | Size of the backing storage (ignored if existingClaim is set) | `100Gi` |
| `storage.storageClassName` | Storage class name (ignored if existingClaim is set) | `""` |
| `storage.accessMode` | Volume access mode (ignored if existingClaim is set) | `ReadWriteOnce` |
| `storage.existingClaim` | Name of existing PVC to use instead of creating new one | `""` |

### Service Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `service.type` | Kubernetes service type | `ClusterIP` |
| `service.nfsPort` | NFS service port | `2049` |
| `service.rpcPort` | RPC portmapper port | `111` |
| `service.mountdPort` | Mount daemon port | `20048` |

### Storage Class Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `storageClass.enabled` | Create NFS storage class | `true` |
| `storageClass.name` | Storage class name | `nfs` |
| `storageClass.reclaimPolicy` | Volume reclaim policy | `Delete` |
| `storageClass.allowVolumeExpansion` | Allow volume expansion | `true` |

### High Availability Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `priorityClass.enabled` | Create priority class | `true` |
| `priorityClass.value` | Priority value | `1000` |
| `podDisruptionBudget.enabled` | Enable PDB | `true` |
| `podDisruptionBudget.maxUnavailable` | Max unavailable pods | `1` |
| `updateStrategy.type` | Update strategy | `Recreate` |

### Monitoring Configuration

| Parameter | Description | Default |
|-----------|-------------|---------|
| `monitoring.enabled` | Enable NFS monitoring | `false` |
| `monitoring.serviceMonitor.enabled` | Create ServiceMonitor | `false` |
| `monitoring.serviceMonitor.interval` | Scrape interval | `30s` |
| `monitoring.nodeExporter.image.repository` | Node exporter image | `prom/node-exporter` |
| `monitoring.nodeExporter.image.tag` | Node exporter version | `v1.6.1` |

## Usage Examples

### Using Existing PVC
```bash
# Create a PVC first
kubectl apply -f - <<EOF
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: my-nfs-storage
spec:
  accessModes:
    - ReadWriteOnce
  resources:
    requests:
      storage: 200Gi
  storageClassName: fast-ssd
EOF

# Then install NFS server using the existing PVC
helm install existing-pvc-nfs soperator/nfs-server \
  --set storage.existingClaim=my-nfs-storage
```

## Monitoring

When monitoring is enabled, the chart deploys a node_exporter sidecar container that exposes NFS-specific metrics:

- NFS server statistics (`nfsd_*`)
- Mount point information
