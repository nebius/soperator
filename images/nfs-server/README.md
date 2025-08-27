# NFS Server

A production-ready NFS server container. Features fast recovery, performance tuning, and graceful shutdown capabilities.

## Environment Variables

### Required Mounts

This image requires two mounts to work properly:
* One for `/etc/exports` file, containing config for exports
* One per each exported directory used in `/etc/exports` file

Example for `/etc/exports`:
```
/exported 10.0.0.0/8(rw,fsid=0,sync,no_subtree_check,no_auth_nlm,insecure,no_root_squash)
```
Then `/exported/` needs to be mounted as a volume as well.

### Optional Variables

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `GRACE_TIME` | NFS grace period in seconds | `10` | - |
| `LEASE_TIME` | NFS lease time in seconds | `10` | - |
| `THREADS` | Number of NFS daemon threads | `8` | -  |

## Basic Usage

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: nfs-server
spec:
  containers:
  - name: nfs-server
    image: your-registry/nfs-server:v1.0.0
    ports:
    - containerPort: 2049
      name: nfs
    - containerPort: 111
      name: portmap
    - containerPort: 20048
      name: mountd
    volumeMounts:
    - name: nfs-storage
      mountPath: /exported
    - name: exports-config
      mountPath: /etc/exports
      subPath: exports
      readOnly: true
    securityContext:
      privileged: true  # Required for NFS server
  volumes:
  - name: nfs-storage
    hostPath:
      path: /path/to/nfs/storage
      volumes:
  - name: exports-config
    configMap:
      name: nfs-server-exports
---
apiVersion: v1
kind: ConfigMap
metadata:
  name: nfs-server-exports
data:
  exports: |
    # NFS exports
    /exported 10.0.0.0/8(rw,fsid=0,sync,no_subtree_check,no_auth_nlm,insecure,no_root_squash)
```
