# Logs Pipeline Documentation

## Overview

The Soperator logs pipeline collects, stores, and provides search capabilities for logs from SLURM jobs, system components, and Kubernetes events. It uses VictoriaLogs for storage and OpenTelemetry collectors for log collection.

## Architecture

```
┌────────────────────────────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│          Jail Filesystem               │     │   K8s Nodes     │     │  K8s Events     │
│ ┌─────────────┐ ┌────────┐ ┌─────────┐ │     │ ┌─────────────┐ │     │ ┌─────────────┐ │
│ │ NCCL Logs   │ │ SLURM  │ │ SLURM   │ │     │ │ System Logs │ │     │ │ K8s Events  │ │
│ │             │ │ Jobs   │ │ Scripts │ │     │ │             │ │     │ │             │ │
│ └─────────────┘ └────────┘ └─────────┘ │     │ └─────────────┘ │     │ └─────────────┘ │
└────────────────────┬───────────────────┘     └────────┬────────┘     └────────┬────────┘
                     │                                  │                       │
         ┌───────────▼───────────┐         ┌────────────▼────────┐   ┌──────────▼────────┐
         │  OTel Collector       │         │  OTel Collector     │   │  OTel Collector   │
         │  (Jail Logs)          │         │  (System Logs)      │   │  (Events)         │
         │  Single Deployment    │         │  DaemonSet          │   │  Deployment       │
         └───────────┬───────────┘         └────────────┬────────┘   └─────────┬─────────┘
                     │                                  │                      │
                     └──────────────────────────────────┴──────────────────────┘
                                                        │
                                            ┌───────────▼─────────────┐
                                            │    VictoriaLogs         │
                                            │    Log Storage          │
                                            │    :9428                │
                                            └────────────┬────────────┘
                                                         │
                                            ┌────────────▼────────────┐
                                            │      Grafana            │
                                            │   Log Exploration       │
                                            └─────────────────────────┘
```

## Components

### Log Collection

#### 1. OpenTelemetry Collector - Jail Logs
- **Purpose**: Collects logs from the jail filesystem
- **Deployment**: Single pod deployment on system nodes
- **Log Sources**:
  - `/opt/soperator-outputs/nccl_logs/` - NCCL debug logs
  - `/opt/soperator-outputs/slurm_jobs/` - SLURM job output logs
  - `/opt/soperator-outputs/slurm_scripts/` - SLURM script logs
- **Poll Interval**: 30s (configurable)
- **Features**: 
  - File path parsing for metadata extraction
  - Efficient single-deployment design for performance

**Troubleshooting:**
```bash
# Check if collector is running
kubectl get pods -n logs-system | grep opentelemetry-collector-jail-logs

# Check collector logs
kubectl logs -n logs-system deployment/opentelemetry-collector-jail-logs

# Verify jail mount
kubectl exec -n logs-system deployment/opentelemetry-collector-jail-logs -- ls /mnt/jail/opt/soperator-outputs/

# Check collector configuration
kubectl get cm -n logs-system opentelemetry-collector-jail-logs -o yaml
```

#### 2. OpenTelemetry Collector - System Logs
- **Purpose**: Collects system logs from nodes
- **Deployment**: DaemonSet (disabled by default)
- **Log Sources**: Node system logs
- **When to Enable**: For debugging node-level issues

#### 3. OpenTelemetry Collector - Events
- **Purpose**: Collects Kubernetes events
- **Deployment**: Single pod deployment
- **Features**: 
  - Filters and forwards K8s events
  - Useful for tracking pod lifecycle and errors

### Log Storage

#### VictoriaLogs
- **Purpose**: Log storage and search engine
- **Port**: 9428
- **Storage**: 30Gi persistent volume
- **Query Language**: LogsQL
- **Features**:
  - Fast full-text search
  - Structured and unstructured log support
  - Low resource consumption
  - Grafana integration

**Connection Example:**
```bash
# Port-forward to VictoriaLogs
kubectl port-forward -n logs-system svc/vm-logs-victoria-logs-single-server 9428:9428

# Search all logs
curl "http://localhost:9428/select/logsql/query?query='*'"

# Search with filters
curl "http://localhost:9428/select/logsql/query?query='_stream:{namespace=\"soperator-system\"} error'"

# Search specific log stream
curl "http://localhost:9428/select/logsql/query?query='_stream:{job=\"slurm_jobs\"} job_id:123456'"

# Time range query
curl "http://localhost:9428/select/logsql/query?query='level:error'&start=2024-01-01T00:00:00Z&end=2024-01-01T01:00:00Z"
```

**Storage Management:**
```bash
# Check storage usage
kubectl exec -n logs-system statefulset/vm-logs-victoria-logs-single-server -- df -h /storage

# Check VictoriaLogs logs
kubectl logs -n logs-system statefulset/vm-logs-victoria-logs-single-server
```

### Log Visualization

#### Grafana Integration
- **Datasource**: VictoriaLogs plugin
- **Features**:
  - Log exploration UI
  - Query builder
  - Log context navigation
  - Integration with metrics dashboards

**Using Grafana for Log Search:**
1. Access Grafana (see metrics-pipeline.md for port-forwarding)
2. Navigate to Explore
3. Select "VictoriaLogs" datasource
4. Use LogsQL queries:

```logsql
# All SLURM job logs
_stream:{job="slurm_jobs"}

# Errors in soperator namespace
_stream:{namespace="soperator-system"} AND level:error

# NCCL logs for specific job
_stream:{job="nccl_logs"} AND job_id:"123456"

# SLURM scripts with specific pattern
_stream:{job="slurm_scripts"} AND "sbatch"

# Kubernetes events
_stream:{job="k8s_events"} AND reason:"Failed"
```


## Log Streams Reference

### Jail Logs Streams

| Stream | Path | Description | Key Fields |
|--------|------|-------------|------------|
| nccl_logs | `/opt/soperator-outputs/nccl_logs/` | NCCL debug and performance logs | job_id, worker_id |
| slurm_jobs | `/opt/soperator-outputs/slurm_jobs/` | SLURM job output (stdout/stderr) | job_id, worker_name |
| slurm_scripts | `/opt/soperator-outputs/slurm_scripts/` | SLURM batch scripts | job_id, script_name |

### Log File Patterns

- **NCCL Logs**: `worker-{N}.job_{ID}.{out,err}`
- **SLURM Jobs**: `worker-{N}.job_{ID}.{out,err}`
- **SLURM Scripts**: `job_{ID}.script`


## Configuration

### Enabling/Disabling Components

In `values.yaml`:

```yaml
observability:
  opentelemetry:
    logs:
      values:
        jailLogs:
          enabled: true  # Jail logs collector
        nodeLogs:
          enabled: false # System logs collector (disabled by default)
```

### Adjusting Poll Interval

```yaml
observability:
  opentelemetry:
    logs:
      values:
        jailLogs:
          pollInterval: 30s  # How often to check for new log files
```

### Storage Configuration

```yaml
observability:
  vmLogs:
    values:
      persistentVolume:
        enabled: true
        size: 30Gi  # Adjust based on log volume
```


## Log Retention

VictoriaLogs retention is controlled by:
- Disk space (30Gi by default)
- Time-based retention policies (if configured)
- Manual cleanup commands

To check storage usage:
```bash
kubectl exec -n logs-system statefulset/vm-logs-victoria-logs-single-server -- df -h /storage
```
