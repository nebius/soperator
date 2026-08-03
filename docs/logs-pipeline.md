# Logs Pipeline Documentation

## Overview

The Soperator logs pipeline collects, stores, and provides search capabilities for logs from SLURM jobs, system components, and Kubernetes events. It uses VictoriaLogs for storage and OpenTelemetry collectors for log collection.

## Architecture

```
┌────────────────────────────────────────┐     ┌─────────────────┐     ┌─────────────────┐
│      Worker Boot Disks (per node)      │     │   K8s Nodes     │     │  K8s Events     │
│      /var/log/soperator-outputs        │     │ ┌─────────────┐ │     │ ┌─────────────┐ │
│ ┌─────────────┐ ┌────────┐ ┌─────────┐ │     │ │ System Logs │ │     │ │ K8s Events  │ │
│ │ NCCL Logs   │ │ SLURM  │ │ SLURM   │ │     │ │             │ │     │ │             │ │
│ │             │ │ Jobs   │ │ Scripts │ │     │ └─────────────┘ │     │ └─────────────┘ │
│ └─────────────┘ └────────┘ └─────────┘ │     └────────┬────────┘     └────────┬────────┘
└────────────────────┬───────────────────┘              │                       │
                     │                                  │                       │
         ┌───────────▼───────────┐         ┌────────────▼────────┐   ┌──────────▼────────┐
         │  OTel Collector       │         │  OTel Collector     │   │  OTel Collector   │
         │  (Jail Logs)          │         │  (System Logs)      │   │  (Events)         │
         │  DaemonSet on workers │         │  DaemonSet          │   │  Deployment       │
         └───────────┬───────────┘         └────────────┬────────┘   └─────────┬─────────┘
                     │                                  │                      │
                     └──────────────────────────────────┴──────────────────────┘
                                                        │
                     ┌──────────────────────────────────┴──────────────────┐
                     │                                                     │
         ┌───────────▼─────────────┐                           ┌───────────▼─────────────┐
         │    VictoriaLogs         │                           │  Nebius Cloud Logging   │
         │    (Local Storage)      │                           │  (OTLP Ingestion)       │
         │    :9428                │                           │  write.logging.*.       │
         │                         │                           │  nebius.cloud:443       │
         └─────────────────────────┘                           └─────────────────────────┘
```

## Cloud Delivery

Logs are delivered to both local VictoriaLogs and Nebius Cloud Logging (when `publicEndpointEnabled: true`), providing immediate local access and long-term cloud storage.

- Local: VictoriaLogs at port 9428 for direct API queries
- Cloud: Nebius Cloud Logging via OTLP/gRPC with bearer token authentication

The public logging endpoint defaults to `dns:///write.logging.eu-north1.nebius.cloud.:443` by design.
`observability.region` does not change the log write region. Set
`observability.opentelemetry.publicEndpoint` only when an explicit endpoint override is needed.

By default, the o11y TSA token writer fetches the write token from IMDS and
maintains it in a Kubernetes Secret. The OpenTelemetry log collectors mount that
Secret and use it through the `bearertokenauth` extension. This avoids requiring
a pre-existing `/mnt/cloud-metadata` token directory on every collector host.
`observability.publicEndpointTokenKind: hostPath` remains available for
installations that intentionally provide the token file from the host.

## Components

### Log Collection

#### 1. OpenTelemetry Collector - Jail Logs
- Purpose: Collects Slurm workload outputs written to each worker's boot disk
- Deployment: DaemonSet on worker nodes; each instance reads (and deletes) only the files written on its own node
- Log Sources: See Centralized Logging Scheme below
- Poll Interval: 30s (configurable)
- Retention: files are deleted after collection once unmodified for `deleteAfterRead.minAge` (default 4h), which is also the on-node debugging window

#### 2. OpenTelemetry Collector - System Logs
- Purpose: Collects system logs from nodes
- Deployment: DaemonSet (disabled by default)
- Log Sources: Node system logs
- When to Enable: For debugging node-level issues

#### 3. OpenTelemetry Collector - Events
- Purpose: Collects Kubernetes events
- Deployment: Single pod deployment

### Collector Runtime Limits

The log collectors set Go runtime limits from their configured pod resources:

- `GOMAXPROCS` is derived from CPU limits when present, otherwise CPU requests.
- CPU quantities are rounded up to a positive whole number: `500m` becomes `1`, `2` stays `2`.
- When `useGoMemLimit` is enabled, the upstream OpenTelemetry collector chart derives `GOMEMLIMIT` from `resources.limits.memory`.
- Upstream `GOMEMLIMIT` targets about 80% of the memory limit and does not fall back to memory requests.
- When `spec.values.useGOMEMLIMIT` is false, the upstream chart does not inject a `GOMEMLIMIT` environment variable.

This applies to the system logs, jail logs, and events collectors. Override values that replace the collector chart values are responsible for setting their own runtime environment.

### Log Storage

#### VictoriaLogs
- Purpose: Log storage and search engine
- Port: 9428
- Storage: 30Gi persistent volume
- Query Language: LogsQL
- Features: Fast full-text search with direct HTTP API access

### Log Querying

#### Direct VictoriaLogs API Access
VictoriaLogs provides a direct HTTP API for log queries using LogsQL syntax:

Connection and Query Examples:
```bash
# Port-forward to VictoriaLogs
kubectl port-forward -n logs-system svc/vm-logs-victoria-logs-single-server 9428:9428

# Search by namespace
curl "http://localhost:9428/select/logsql/query?query=k8s.namespace.name:soperator-system"

# Search with text pattern
curl "http://localhost:9428/select/logsql/query?query=failed"

# Search by pod name
curl "http://localhost:9428/select/logsql/query?query=k8s.pod.name:controller-0"

# Time range query
curl "http://localhost:9428/select/logsql/query?query=level:error&start=2024-01-01T00:00:00Z&end=2026-01-01T01:00:00Z"
```

## Centralized Logging Scheme

Soperator implements a centralized logging system that automatically collects and categorizes Slurm workload outputs. Logs are organized by type and processed by OpenTelemetry collectors for centralized analysis.

### Directory Structure

The outputs tree inside the jail is split by storage locality. Directories under `local/` are node-local: each worker binds its boot-disk directory `/var/log/soperator-outputs` into its jail view, so writes never touch the shared filesystem and files are only visible on the worker that wrote them. Directories under `shared/` live on the shared jail filesystem.

```
/opt/soperator-outputs/
├── local/                          # node-local (worker boot disk)
│   ├── nccl_logs/                  # NCCL debug outputs (SPANK plugin)
│   ├── slurm_jobs/                 # Active check job outputs (gpu-checks, all-reduce-perf-nccl, ...)
│   ├── slurm_scripts/              # Slurm hook outputs (prolog, epilog, HealthCheckProgram)
│   ├── task_prolog/                # Per-task prolog outputs
│   └── health_checker_cmd_stdout/  # Raw health-checker test command stdout
└── shared/                         # shared jail filesystem
    ├── nccl_profiles/              # NCCL Inspector profiling dumps
    └── acceptance/                 # e2e acceptance job outputs
```

### Logging Schema

Log files include the worker name at the beginning of the filename for easy identification:

**NCCL Logs:**
```
worker_name.job_id.job_step_id.out
Example: worker-0.12345.67890.out
```

**Active Check Jobs:**
```
worker_name.job_name.job_id[.array_id].out
Examples:
- worker-1.all-reduce-perf-nccl.12345.out
- worker-2.soperator-gpu-checks.12345.1.out (array job)
```

**Slurm Scripts:**
```
worker_name.script_name.context.out
Examples:
- worker-0.health_checker.prolog.out
- worker-3.drop_page_cache.epilog.out
```

### Generated Labels

The logging system automatically extracts metadata from filenames and creates the following labels:

- `slurm_node_name`: Slurm worker node identifier extracted from filename (e.g., "worker-0", "worker-1")
- `log_type`: Category (nccl_logs, slurm_jobs, slurm_scripts, task_prolog, health_checker_cmd_stdout)
- `job_id`, `job_step_id`: For NCCL and task prolog logs
- `job_name`, `job_array_id`: For Slurm job logs
- `slurm_script_name`, `slurm_script_context`: For script logs
- `health_checker.check_name`, `health_checker.run_id`: For raw health-checker outputs


## Configuration

```yaml
observability:
  # Cloud delivery
  publicEndpointEnabled: true  # Enable/disable cloud export
  publicEndpointTokenKind: secret
  logsProjectId: "your-nebius-project-id"
  region: "eu-north1"
  tsaToken:
    secretName: o11y-writer-sa-token
    secretKey: accessToken
    writer:
      source: imds
      namespaces: []  # Defaults to enabled metrics/log collector namespaces
  opentelemetry:
    # Optional. Defaults to dns:///write.logging.eu-north1.nebius.cloud.:443
    publicEndpoint: "dns:///write.logging.eu-north1.nebius.cloud.:443"
    batch:
      timeout: 1s
      sendBatchSize: 2000
      sendBatchMaxSize: 5000
  
  # Storage
  vmLogs:
    values:
      persistentVolume:
        enabled: true
        size: 30Gi  # Adjust based on log volume
```

## Log Retention

VictoriaLogs retention is controlled by disk space (30Gi by default) and optional time-based policies.

Check storage usage:
```bash
kubectl exec -n logs-system statefulset/vm-logs-victoria-logs-single-server -- df -h /storage
```

On worker boot disks, collected files under `/var/log/soperator-outputs` are deleted by the jail-logs collector after they have been unmodified for `deleteAfterRead.minAge` (default 4h). Replacing a worker node discards any files not yet collected from it.
