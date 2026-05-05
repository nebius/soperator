# SLURM Exporter

## Overview

The SLURM Exporter is a component of Soperator that collects metrics from SLURM clusters and exports them in Prometheus format. It provides comprehensive monitoring capabilities for SLURM cluster health, job status, node states, and controller performance metrics.

The exporter integrates seamlessly with the Prometheus monitoring stack and enables observability for SLURM workloads running on Kubernetes through Soperator.

### Key Features

- **Asynchronous metrics collection** with configurable intervals (default: 30s)
- **Real-time monitoring** of SLURM nodes, jobs, and controller performance
- **Prometheus-native metrics** with standardized naming conventions
- **Rich labeling** for detailed filtering and aggregation
- **Controller RPC diagnostics** similar to SLURM's `sdiag` command
- **Kubernetes-native deployment** as part of Soperator

## Configuration

The SLURM Exporter can be configured using either command-line flags or environment variables. Environment variables take precedence over defaults but are overridden by explicitly provided command-line flags.

### Configuration Priority

The configuration follows this priority order:
1. **Command-line flags** (highest priority) - Explicitly provided flags override all other settings
2. **Environment variables** - Used when flags are not provided
3. **Default values** (lowest priority) - Used when neither flags nor environment variables are set

### Configuration Options

All configuration options can be set via command-line flags or environment variables:

| Environment Variable | Flag | Description | Default |
|---------------------|------|-------------|---------|
| `SLURM_EXPORTER_CLUSTER_NAME` | `--cluster-name` | The name of the SLURM cluster (required) | *none* |
| `SLURM_EXPORTER_CLUSTER_NAMESPACE` | `--cluster-namespace` | The namespace of the SLURM cluster | `soperator` |
| `SLURM_EXPORTER_SLURM_API_SERVER` | `--slurm-api-server` | The address of the SLURM REST API server | `http://localhost:6820` |
| `SLURM_EXPORTER_COLLECTION_INTERVAL` | `--collection-interval` | How often to collect metrics from SLURM APIs | `30s` |
| `SLURM_EXPORTER_METRICS_BIND_ADDRESS` | `--metrics-bind-address` | Address for the main metrics endpoint | `:8080` |
| `SLURM_EXPORTER_MONITORING_BIND_ADDRESS` | `--monitoring-bind-address` | Address for the self-monitoring metrics endpoint | `:8081` |
| `SLURM_EXPORTER_LOG_FORMAT` | `--log-format` | Log format: `plain` or `json` | `json` |
| `SLURM_EXPORTER_LOG_LEVEL` | `--log-level` | Log level: `debug`, `info`, `warn`, `error` | `debug` |
| `SLURM_EXPORTER_JOB_SOURCE` | `--job-source` | EXPERIMENTAL: Source for job data: `controller` (Slurm controller API — current behavior) or `accounting` (Slurm accounting API / slurmdbd). Use `accounting` when the controller endpoint is overloaded on large clusters. | `controller` |
| `SLURM_EXPORTER_ACCOUNTING_JOB_STATES` | `--accounting-job-states` | EXPERIMENTAL: When `--job-source=accounting`, CSV of Slurm job state strings forwarded verbatim to the accounting API's `state` query parameter (e.g. `RUNNING,PENDING`). Empty = no state filter. Common values: `PENDING`, `RUNNING`, `COMPLETED`, `FAILED`, `CANCELLED`, `TIMEOUT` — see the [Slurm `sacct -s` reference](https://slurm.schedmd.com/sacct.html#OPT_state). **Note:** slurmdbd applies this filter to the *historical* states a job held during the lookback window (same as `sacct --state=...`), not to the current state of the returned jobs. A job that was RUNNING during the window and has since completed will still be returned with its current state set to e.g. `COMPLETED`. | *empty* |
| `SLURM_EXPORTER_ACCOUNTING_JOBS_LOOKBACK` | `--accounting-jobs-lookback` | EXPERIMENTAL: When `--job-source=accounting`, the size of the time window queried from the accounting API. The query uses `[now − lookback, now + 5 min]`. Long-running jobs that started before the window are still returned — the accounting API selects any job whose lifetime overlaps the window. The +5 min skew tolerates clock drift between slurmrestd, slurmctld and slurmdbd. | `1h` |

### Job source: controller vs accounting

The exporter can collect job metrics from either the Slurm controller (`slurmctld`) or the accounting daemon (`slurmdbd`):

- **`controller`** (default) — reads jobs directly from `slurmctld`. Lowest latency and matches what `squeue` returns. This is the recommended source for most clusters.
- **`accounting`** — reads jobs from `slurmdbd`, matching what `sacct` returns. Use this on large clusters where `slurmctld` is under heavy load and the controller's job-info RPC is contributing to that load. The exporter queries a sliding time window (`--accounting-jobs-lookback`); long-running jobs that started before the window are still included because `sacct` selects any job whose lifetime overlaps the window.

The two sources are mutually exclusive. The `accounting` path lets operators switch the exporter off `slurmctld` when controller-side load becomes a problem, without losing pending jobs (the accounting API also returns pending jobs).

#### Known limitations of the accounting source

The accounting API is a useful fallback but does not carry every label the controller path produces. If a dashboard depends on any of the following, scrape from `controller` mode for that view:

- **`user_id` is empty.** Slurmdbd's v0.0.41 OpenAPI projects only the resolved username, not the numeric uid. The exporter does not reverse-look-up uids because the cluster's uid mapping is not necessarily the exporter container's, and silent disagreement between deployments is worse than an empty label.
- **`job_state_reason` is `None` for pending jobs.** Slurmdbd persists `state_reason_prev` (frozen at the last state transition), not the live current pending reason. A long-pending job will read as `None` or stale even though `slurmctld` would expose `PartitionNodeLimit`, `Resources`, etc.
- **Array-job tasks may appear collapsed.** Completed and running array tasks are returned as individual rows when their lifetime overlaps the lookback window — widen `--accounting-jobs-lookback` if some tasks are missing. Pending array tasks, however, are collapsed by slurmdbd into a single master record carrying an `array.task` range expression (e.g. `1-5`); the exporter does not expand the range into per-task series, but it does surface the expression in the `array_task_id` label so the master record retains its array identity (the label carries either a single task index like `3` or a range like `1-5`). Per-task fan-out would require parsing the range and synthesizing individual rows — tracked separately for a follow-up.

#### Accounting response is not exposed as-is

To keep accounting-mode metrics aligned with controller-mode metrics for the labels dashboards typically join on, the exporter applies the following adjustments to the slurmdbd response before it reaches Prometheus. These are intentional, but they do mean the values you see are not strictly faithful to what slurmdbd returned on the wire:

- **`end_time` for RUNNING jobs is projected as `start_time + time_limit`.** Slurmdbd stores `time_end=0` for unfinished jobs, while slurmctld returns this exact projection on the controller path. Mirroring it here keeps `slurm_job_info.end_time` semantically consistent across sources. Caveat: projection requires slurmdbd to actually carry `time_limit` for the job. If slurmdbd recorded the job without a resolved time limit (jobs that ran with the partition's default `MaxTime` may not have it persisted, depending on slurmdbd version and config), or if the job was submitted with `--time=UNLIMITED`, `end_time` stays empty — same as the controller does for jobs without a time limit.
- **Unallocated `nodes` strings are normalized to empty.** Slurmdbd returns the literal string `"None assigned"` (and historically `"(null)"`) for jobs with no nodes assigned yet. The exporter treats these as no-nodes so the `slurm_node_job` metric does not emit synthetic `node_name="None assigned"` edges, which would otherwise inflate Prometheus series cardinality for every pending job ever scraped.
- **CPU and memory are read from `tres_req` when `tres_alloc` is empty.** Pending and DEADLINE-without-allocation jobs have no `tres_alloc`, and slurmdbd's `Required.CPUs` is the per-task minimum (defaults to 1), not the total. Falling back to `tres_req.cpu` / `tres_req.mem` keeps `slurm_job_cpus` and `slurm_job_memory_bytes` reporting the value the user actually requested. Allocated TRES still wins when present.
- **Stale zombie-pending rows are dropped.** Slurmdbd's filter for jobs without an explicit state keeps any record with `time_end=0` forever, regardless of submit age, because of an `OR` in the underlying SQL. The exporter drops rows that were never started, never ended, and were submitted more than 30 days ago. These almost always come from a `scancel` that didn't propagate or a controller crash that lost in-flight termination RPCs; without the filter every such record becomes a permanent Prometheus series until slurmdbd's `PurgeJobAfter` sweeps them. **Caveat:** the 30-day threshold can produce false positives. Genuinely long-pending jobs (long resource queues, jobs held with `scontrol hold` and not released) submitted more than 30 days ago will be dropped from accounting-mode metrics. Such jobs remain visible via the controller source, which has no equivalent age cap. The threshold is a conservative trade-off against the unbounded cardinality of `time_end=0` zombies; if a deployment has many genuinely long-pending jobs, the constant in `internal/slurmapi/client.go` is the knob to revisit.

## Exported Metrics

### Node State Model

Slurm represents node state as a 32-bit integer where:
- The lowest 4 bits encode 6 mutually exclusive base states: `IDLE`, `DOWN`, `ALLOCATED`, `ERROR`, `MIXED`, `UNKNOWN`
- Additional bits are flag bits that can be combined with base states: `COMPLETING`, `DRAIN`, `MAINTENANCE`, `RESERVED`, `FAIL`, `PLANNED`

For example, a node can be `IDLE+DRAIN` (idle but marked for draining) or `ALLOCATED+COMPLETING` (running jobs but finishing up).

Reference: https://github.com/SchedMD/slurm/blob/master/slurm/slurm.h.in

The exporter represents this as:
- `state_base` label: The single base state (IDLE, ALLOCATED, etc.)
- `state_is_*` labels: Boolean flags for additional state flags

Boolean state flag label convention:
- Legacy flags (`state_is_drain`, `state_is_maintenance`, `state_is_reserved`): Use `"true"`/`"false"` for backward compatibility
- New flags (`state_is_completing`, `state_is_fail`, `state_is_planned`): Use `"true"` or empty string (`""`) to reduce label cardinality (in Victoria Metrics, empty label value === no label, helping avoid the 30 label limit)

### Core Metrics (Node and Job)

| Metric Name & Type | Description & Labels |
|-------------------|---------------------|
| **slurm_node_info**<br>*Gauge* | Provides detailed information about SLURM nodes<br><br>**Labels:**<br>• `node_name` - Name of the SLURM node<br>• `instance_id` - Kubernetes instance identifier<br>• `state_base` - Base node state (IDLE, ALLOCATED, DOWN, ERROR, MIXED, UNKNOWN)<br>• `state_is_drain` - Whether node is in drain state ("true"/"false")<br>• `state_is_maintenance` - Whether node is in maintenance state ("true"/"false")<br>• `state_is_reserved` - Whether node is in reserved state ("true"/"false")<br>• `state_is_completing` - Whether node is in completing state ("true" or empty)<br>• `state_is_fail` - Whether node is in fail state ("true" or empty)<br>• `state_is_planned` - Whether node is in planned state ("true" or empty)<br>• `state_is_not_responding` - Whether the node is marked as not responding ("true" or empty)<br>• `state_is_invalid` - Whether the node state is considered invalid by SLURM ("true" or empty)<br>• `is_unavailable` - Computed by the exporter: "true" when the node is considered unavailable (DOWN+* or IDLE+DRAIN+*), empty string otherwise<br>• `reservation_name` - Reservation that currently includes the node (trimmed to 50 characters)<br>• `address` - IP address of the node<br>• `reason` - Reason for current node state (empty string if node has no reason set)<br>• `comment` - Comment set on the node (e.g., by active checks when GPU health check fails) |
| **slurm_node_gpu_seconds_total**<br>*Counter* | Total GPU seconds accumulated on SLURM nodes<br><br>**Labels:**<br>• `node_name` - Name of the SLURM node<br>• `state_base` - Base node state<br>• `state_is_drain` - Drain state flag<br>• `state_is_maintenance` - Maintenance state flag<br>• `state_is_reserved` - Reserved state flag |
| **slurm_node_fails_total**<br>*Counter* | Total number of node state transitions to failed states (DOWN/DRAIN)<br><br>**Labels:**<br>• `node_name` - Name of the SLURM node<br>• `state_base` - Base node state at time of failure<br>• `state_is_drain` - Drain state flag<br>• `state_is_maintenance` - Maintenance state flag<br>• `state_is_reserved` - Reserved state flag<br>• `reason` - Reason for the node failure |
| **slurm_node_unavailability_duration_seconds**<br>*Histogram* | Duration of completed node unavailability events (DOWN+* or IDLE+DRAIN+*)<br><br>**Labels:**<br>• `node_name` - Name of the SLURM node<br><br>**Note:** Observations are recorded when unavailability events complete. Duration tracking is reset on exporter restarts, which may affect accuracy |
| **slurm_node_draining_duration_seconds**<br>*Histogram* | Duration of completed node draining events (DRAIN+ALLOCATED or DRAIN+MIXED)<br><br>**Labels:**<br>• `node_name` - Name of the SLURM node<br><br>**Note:** Observations are recorded when draining events complete. Duration tracking is reset on exporter restarts, which may affect accuracy |
| **slurm_node_cpus_total**<br>*Gauge* | Total number of CPUs on the node<br><br>**Labels:**<br>• `node_name` - Name of the SLURM node |
| **slurm_node_cpus_allocated**<br>*Gauge* | Number of CPUs currently allocated on the node<br><br>**Labels:**<br>• `node_name` - Name of the SLURM node |
| **slurm_node_cpus_idle**<br>*Gauge* | Number of idle CPUs on the node<br><br>**Labels:**<br>• `node_name` - Name of the SLURM node |
| **slurm_node_cpus_effective**<br>*Gauge* | Effective CPUs on the node (excluding specialized CPUs reserved for system daemons)<br><br>**Labels:**<br>• `node_name` - Name of the SLURM node |
| **slurm_node_memory_total_bytes**<br>*Gauge* | Total memory on the node in bytes<br><br>**Labels:**<br>• `node_name` - Name of the SLURM node |
| **slurm_node_memory_allocated_bytes**<br>*Gauge* | Allocated memory on the node in bytes<br><br>**Labels:**<br>• `node_name` - Name of the SLURM node |
| **slurm_node_memory_free_bytes**<br>*Gauge* | Free memory on the node in bytes<br><br>**Labels:**<br>• `node_name` - Name of the SLURM node |
| **slurm_node_memory_effective_bytes**<br>*Gauge* | Effective memory on the node in bytes (total minus specialized memory reserved for system daemons)<br><br>**Labels:**<br>• `node_name` - Name of the SLURM node |
| **slurm_node_partition**<br>*Gauge* | Maps nodes to their partitions, enabling partition-level aggregation via PromQL joins<br><br>**Labels:**<br>• `node_name` - Name of the SLURM node<br>• `partition` - Name of the SLURM partition |
| **slurm_job_info**<br>*Gauge* | Detailed information about SLURM jobs<br><br>**Labels:**<br>• `job_id` - SLURM job identifier<br>• `job_state` - Current job state (PENDING, RUNNING, COMPLETED, FAILED, etc.)<br>• `job_state_reason` - Reason for current job state<br>• `slurm_partition` - SLURM partition name<br>• `job_name` - User-defined job name<br>• `user_name` - Username who submitted the job<br>• `user_id` - Numeric user ID who submitted the job<br>• `standard_error` - Path to stderr file<br>• `standard_output` - Path to stdout file<br>• `array_job_id` - Array job ID (if applicable)<br>• `array_task_id` - Array task identity. Either a single task index (e.g. `3`) for an exploded task or a range expression (e.g. `1-5`, `1,3,5-9:2`) for a collapsed array master record on the accounting path. Empty for non-array jobs.<br>• `submit_time` - When the job was submitted (Unix timestamp seconds, empty if not available or zero)<br>• `start_time` - When the job started execution (Unix timestamp seconds, empty if not available or zero)<br>• `end_time` - When the job completed (Unix timestamp seconds, empty if not available or zero). **Warning:** For non-terminal states like RUNNING, this may contain a future timestamp representing the forecasted end time based on the job's time limit<br>• `finished_time` - When the job actually finished for terminal states only (Unix timestamp seconds, empty for non-terminal states or if end_time is zero). Unlike `end_time`, this field only contains actual completion times, never forecasted values |
| **slurm_node_job**<br>*Gauge* | Mapping between jobs and the nodes they're running on<br><br>**Labels:**<br>• `job_id` - SLURM job identifier<br>• `node_name` - Name of the node running the job |
| **slurm_job_duration_seconds**<br>*Gauge* | Job duration in seconds. For running jobs, this is the time elapsed since the job started. For completed jobs, this is the total execution time.<br><br>**Labels:**<br>• `job_id` - SLURM job identifier<br><br>**Notes:**<br>• Only exported for jobs with a valid start time<br>• For non-terminal states (RUNNING, etc.): duration = current_time - start_time<br>• For terminal states (COMPLETED, FAILED, etc.): duration = end_time - start_time (only if end_time is valid) |
| **slurm_job_cpus**<br>*Gauge* | Number of CPUs allocated to the job<br><br>**Labels:**<br>• `job_id` - SLURM job identifier |
| **slurm_job_memory_bytes**<br>*Gauge* | Memory allocated to the job in bytes<br><br>**Labels:**<br>• `job_id` - SLURM job identifier |

### Controller RPC Metrics

These metrics provide insights into SLURM controller performance, similar to the output of the `sdiag` command, and were implemented to address [issue #1027](https://github.com/nebius/soperator/issues/1027).

| Metric Name & Type | Description & Labels |
|-------------------|---------------------|
| **slurm_controller_rpc_calls_total**<br>*Counter* | Total count of RPC calls by message type<br><br>**Labels:**<br>• `message_type` - Type of RPC message (e.g., REQUEST_NODE_INFO, REQUEST_JOB_INFO, REQUEST_PING) |
| **slurm_controller_rpc_duration_seconds_total**<br>*Counter* | Total time spent processing RPCs by message type (converted from microseconds)<br><br>**Labels:**<br>• `message_type` - Type of RPC message |
| **slurm_controller_rpc_user_calls_total**<br>*Counter* | Total count of RPC calls by user<br><br>**Labels:**<br>• `user` - Username making the RPC calls<br>• `user_id` - Numeric user ID |
| **slurm_controller_rpc_user_duration_seconds_total**<br>*Counter* | Total time spent on user RPCs (converted from microseconds)<br><br>**Labels:**<br>• `user` - Username making the RPC calls<br>• `user_id` - Numeric user ID |
| **slurm_controller_server_thread_count**<br>*Gauge* | Number of server threads in the SLURM controller<br><br>**Labels:** None |

### Self-Monitoring Metrics

The exporter provides self-monitoring metrics to track its own health and performance. These metrics are available on a separate endpoint (default port 8081) to avoid mixing operational metrics with business metrics.

| Metric Name & Type | Description & Labels |
|-------------------|---------------------|
| **slurm_exporter_collection_duration_seconds**<br>*Gauge* | Duration of the most recent metrics collection from SLURM APIs<br><br>**Labels:** None |
| **slurm_exporter_collection_attempts_total**<br>*Counter* | Total number of metrics collection attempts<br><br>**Labels:** None |
| **slurm_exporter_collection_failures_total**<br>*Counter* | Total number of failed metrics collection attempts<br><br>**Labels:** None |
| **slurm_exporter_metrics_requests_total**<br>*Counter* | Total number of requests to the `/metrics` endpoint<br><br>**Labels:** None |
| **slurm_exporter_metrics_exported**<br>*Gauge* | Number of metrics exported in the last scrape<br><br>**Labels:** None |

### Accessing Self-Monitoring Metrics

To access self-monitoring metrics:

```bash
# Default monitoring port
curl http://localhost:8081/metrics

# Or with custom monitoring address
./soperator-exporter --monitoring-bind-address=:9090
curl http://localhost:9090/metrics
```

## Local Development

To run the exporter locally against a cluster for debugging:

1. Port-forward the SLURM REST API service:
```bash
kubectl port-forward -n soperator svc/soperator-rest-svc 6820:6820
```

2. Run the exporter (it finds the JWT secret in the cluster automatically):
```bash
go run ./cmd/exporter/main.go --cluster-name=soperator --kubeconfig-path=$HOME/.kube/config
```

3. View the metrics:
```bash
curl localhost:8080/metrics
```

## Grafana Dashboard Example

The SLURM Exporter integrates with existing Grafana dashboards. Here's an example based on the production dashboard from [nebius-solutions-library](https://github.com/nebius/nebius-solutions-library/blob/release/soperator/soperator/modules/monitoring/templates/dashboards/cluster_health.json).
