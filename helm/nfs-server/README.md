# NFS Server Helm Chart

A Helm chart for deploying an NFS server on Kubernetes with built-in monitoring capabilities.

## Features

- **StatefulSet**: NFS server can be enabled or scaled to zero with persistent storage via separate PVC
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
| `nfs.maxConnections` | Server connection limit where supported by the host kernel; `0` uses the kernel default | `8192` |
| `nfs.replicas` | Number of NFS server replicas. Must be `0` or `1` | `1` |

### Connection Limit

The chart passes `nfs.maxConnections` to the server as `MAX_CONNECTIONS`. The server
writes it to `/proc/fs/nfsd/max_connections` after starting `rpc.nfsd` and before
exporting filesystems. If the host kernel no longer exposes this setting, the server
logs that it is skipping it. Changing the Helm value rolls the NFS server pod.

The parameter is optional: upgrades using `--reuse-values` from releases without
it use `8192`. An explicit `0` is preserved and selects the kernel default.

Provisioning should calculate this value from the expected peak client count and
the actual client `nconnect` setting. For kernels with a total connection limit,
`max(8192, 2 * client_count * nconnect)` provides a default floor and 2x connection
headroom. Count independent client connection pools (usually mounting Kubernetes
nodes), including login nodes, rather than pods or PVCs that share connections.
During a mount-option rollout, include clients still using the old `nconnect` value.

The chart's StorageClass defaults to `nconnect=8`. Existing overrides of
`storageClass.mountOptions` must be updated separately to use this value.

For example, a Terraform `helm_release` can include the calculated value in its
`values` argument:

```hcl
values = [yamlencode({
  nfs = {
    maxConnections = max(8192, 2 * var.nfs_client_count * var.nfs_nconnect)
  }
})]
```

`var.nfs_nconnect` must match the actual mount option; this example does not change
mount options. When provisioning through `helm/soperator-fluxcd`, pass the same
`nfs.maxConnections` value under `nfsServer.overrideValues`.

### Storage Configuration

The chart creates a dedicated PersistentVolumeClaim (PVC) for storage, which provides more flexibility than StatefulSet volumeClaimTemplates (allows label changes, resizing, etc.).

| Parameter | Description | Default |
|-----------|-------------|---------|
| `storage.size` | Size of the backing storage (ignored if existingClaim is set) | `100Gi` |
| `storage.storageClassName` | Storage class name (ignored if existingClaim is set) | `""` |
| `storage.accessMode` | Volume access mode (ignored if existingClaim is set) | `ReadWriteOnce` |
| `storage.existingClaim` | Name of existing PVC to use instead of creating new one | `""` |

**Note**: When `storage.existingClaim` is not specified, the chart creates a PVC named `<release-name>-storage`. When `storage.existingClaim` is provided, that PVC is used instead and no new PVC is created.

**Note**: This chart supports either one active NFS server replica or zero replicas for scale-down/maintenance. Multiple NFS server replicas are not supported.

### Service Configuration

The service type is `ClusterIP`.

| Parameter | Description | Default |
|-----------|-------------|---------|
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
