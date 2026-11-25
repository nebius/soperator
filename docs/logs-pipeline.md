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

The three collectors send to local VictoriaLogs on port 9428 by default. Each collector can also send to Nebius Cloud Logging over OTLP/gRPC with bearer token authentication. Specify the regional endpoint and the resolved project ID in the collector's exporter settings.

With `observability.publicEndpointEnabled: true` and `observability.publicEndpointTokenKind: secret`, the TSA token writer fetches the token from IMDS by default and stores it in a Kubernetes Secret. Each cloud-enabled collector specifies its own token volume, mount, and `bearertokenauth` extension. Configure a Secret volume for the writer-managed token or a hostPath volume when the token is supplied on the node.

## Components

### Log Collection

#### 1. OpenTelemetry Collector - Jail Logs

A DaemonSet collects Slurm workload outputs from each worker's boot disk. The two jail log receivers each set `poll_interval` to `30s` by default and use their own `delete_after_read` and `delete_after_read_min_age`. [Centralized Logging Scheme](#centralized-logging-scheme)

#### 2. OpenTelemetry Collector - System Logs

A DaemonSet collects Kubernetes pod logs and node system logs through the `filelog/pods` and `filelog/nodelogs` receivers in the `logs` pipeline.

#### 3. OpenTelemetry Collector - Events
- Purpose: Collects Kubernetes events
- Deployment: Single pod deployment

### Collector Runtime Limits

The three log collectors set `GOMAXPROCS` to `"1"` in their default `extraEnvs`. When overriding it for different resources, use the CPU limit or the CPU request when there is no limit, rounded up to at least one process. For example, use `"1"` for `500m` and `"2"` for `2` cores. Carry over the other environment entries when replacing `extraEnvs`.

With the native `useGOMEMLIMIT` flag enabled, the upstream chart derives `GOMEMLIMIT` from `resources.limits.memory` and targets about 80% of the limit. Both the flag and a memory limit are required for automatic injection.

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

Configure the collectors independently under `observability.opentelemetry.logs.values`, `observability.opentelemetry.jailLogs.values`, and `observability.opentelemetry.events.values`. To omit node logs, set `logs.values.config.receivers.filelog/nodelogs` to `null` and remove that receiver from `config.service.pipelines.logs.receivers`. Remove the `node-logs` volume and mount and any supplemental groups used to read those logs.

Each exporter has its own `sending_queue`. By default the VictoriaLogs queues hold 30,000 records, run 10 consumers, and batch 2,000 to 5,000 records. Queue files live under `/var/lib/otelcol`, `/var/lib/otelcol-jail`, and `/var/lib/otelcol-events` for the three collectors, respectively. To disable file storage, set the extension and its receiver and queue `storage` references to `null`. Remove it from `config.service.extensions` and remove its volume, mounts, and initialization container in the same change.

This example adds Cloud Logging to the events collector with the shared Secret. Include the public exporter in the corresponding pipeline for each additional collector. Helm replaces whole lists, so copy the entries for the local pipeline and storage into any overridden lists.

```yaml
observability:
  publicEndpointEnabled: true
  publicEndpointTokenKind: secret
  opentelemetry:
    events:
      dependsOn:
        - name: '{{ include "soperator-fluxcd.fullname" . }}-ns'
        - name: '{{ include "soperator-fluxcd.fullname" . }}-vm-logs'
        - name: '{{ include "soperator-fluxcd.o11yTsaTokenWriterName" . }}'
      values:
        extraVolumes:
          - name: varlibotelcol
            hostPath:
              path: /var/lib/otelcol-events
              type: DirectoryOrCreate
          - name: o11ytoken
            secret:
              secretName: o11y-writer-sa-token
        extraVolumeMounts:
          - name: varlibotelcol
            mountPath: /var/lib/otelcol-events
          - name: o11ytoken
            mountPath: /o11ytoken
            readOnly: true
        config:
          exporters:
            otlp:
              endpoint: dns:///write.logging.eu-north1.nebius.cloud.:443
              balancer_name: round_robin
              compression: snappy
              retry_on_failure:
                enabled: true
                initial_interval: 200ms
                max_elapsed_time: 0
              timeout: 5s
              headers:
                iam-container: your-nebius-project-id
              auth:
                authenticator: bearertokenauth
              sending_queue:
                enabled: true
                sizer: items
                queue_size: 30000
                num_consumers: 10
                storage: file_storage
                batch:
                  sizer: items
                  flush_timeout: 1s
                  min_size: 500
                  max_size: 1000
          extensions:
            bearertokenauth:
              filename: /o11ytoken/accessToken
          service:
            extensions: [health_check, file_storage, bearertokenauth]
            pipelines:
              logs/events:
                exporters: [otlp_http/victoriametrics, otlp]

  # Storage
  vmLogs:
    values:
      server:
        persistentVolume:
          enabled: true
          size: 30Gi
```

Keep the Cloud Logging batch at or below 1,000 records. For jail-logs dependencies and migration from shared collector settings, see the chart guide. [OpenTelemetry collector upgrades](../helm/soperator-fluxcd/README.md#opentelemetry-collector-upgrades)

## Log Retention

VictoriaLogs retention is controlled by disk space (30Gi by default) and optional time-based policies.

Check storage usage:
```bash
kubectl exec -n logs-system statefulset/vm-logs-victoria-logs-single-server -- df -h /storage
```

On worker boot disks, the jail-logs collector deletes collected files under `/var/log/soperator-outputs` after they have been unmodified for four hours. The minimum age is set by `delete_after_read_min_age` in `jailLogs.values.config.receivers.filelog` and `jailLogs.values.config.receivers.filelog/health_checker`. Keep automatic deletion confined to the worker-local log paths. Set `delete_after_read: false` on both receivers to keep collected files on disk. Replacing a worker node discards any files still on its boot disk.
