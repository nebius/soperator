# NFS Server

A production-ready NFS server container. Features fast recovery, performance tuning, and graceful shutdown capabilities.

## Environment Variables

### Required Variables

| Variable | Description | Example |
|----------|-------------|---------|
| `SHARED_DIRECTORY` | Path to the directory to export | `/exports` |

### Optional Variables

| Variable | Description | Default | Example |
|----------|-------------|---------|---------|
| `PERMITTED` | Allowed client IPs/networks | `*` | `10.0.0.0/8` |
| `READ_ONLY` | Enable read-only mode | `""` (false) | `"true"` |
| `SYNC` | Enable synchronous writes | `""` (async) | `"true"` |
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
    env:
    - name: SHARED_DIRECTORY
      value: "/exports"
    ports:
    - containerPort: 2049
      name: nfs
    - containerPort: 111
      name: portmap
    - containerPort: 20048
      name: mountd
    volumeMounts:
    - name: nfs-storage
      mountPath: /exports
    securityContext:
      privileged: true  # Required for NFS server
  volumes:
  - name: nfs-storage
    hostPath:
      path: /path/to/nfs/storage
```
