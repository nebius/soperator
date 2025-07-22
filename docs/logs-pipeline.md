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

## Components

### Log Collection

#### 1. OpenTelemetry Collector - Jail Logs
- Purpose: Collects logs from the jail filesystem
- Deployment: Single pod deployment on system nodes
- Log Sources: See Centralized Logging Scheme below
- Poll Interval: 30s (configurable)

#### 2. OpenTelemetry Collector - System Logs
- Purpose: Collects system logs from nodes
- Deployment: DaemonSet (disabled by default)
- Log Sources: Node system logs
- When to Enable: For debugging node-level issues

#### 3. OpenTelemetry Collector - Events
- Purpose: Collects Kubernetes events
- Deployment: Single pod deployment

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

Logs are organized in a flat structure by log type:

```
/opt/soperator-outputs/
├── nccl_logs/      # NCCL debug outputs from all workers
├── slurm_jobs/     # Active check jobs (all-reduce-perf-nccl) from all workers
└── slurm_scripts/  # SLURM hook outputs (prolog, epilog, HealthCheckProgram) from all workers
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
- worker-2.enroot-cleanup.12345.1.out (array job)
```

**Slurm Scripts:**
```
worker_name.script_name.context.out
Examples:
- worker-0.health_checker.prolog.out
- worker-3.cleanup_enroot.epilog.out
```

### Generated Labels

The logging system automatically extracts metadata from filenames and creates the following labels:

- `slurm_node_name`: Slurm worker node identifier extracted from filename (e.g., "worker-0", "worker-1")
- `log_type`: Category (nccl_logs, slurm_jobs, slurm_scripts)
- `job_id`, `job_step_id`: For NCCL logs
- `job_name`, `job_array_id`: For Slurm job logs
- `slurm_script_name`, `slurm_script_context`: For script logs


## Configuration

```yaml
observability:
  # Cloud delivery
  publicEndpointEnabled: true  # Enable/disable cloud export
  projectId: "your-nebius-project-id"
  region: "eu-north1"
  
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
