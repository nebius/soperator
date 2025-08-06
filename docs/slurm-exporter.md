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

### Command Line Flags

When running the exporter directly, you can configure various settings:

```bash
./soperator-exporter \
  --collection-interval=30s \
  --metrics-bind-address=:8080 \
  --monitoring-bind-address=:8081
```

- `--collection-interval`: How often to collect metrics from SLURM APIs (default: 30s)
- `--metrics-bind-address`: Address for the main metrics endpoint (default: :8080)
- `--monitoring-bind-address`: Address for the self-monitoring metrics endpoint (default: :8081)

## Exported Metrics

### Node Metrics

#### Gauge `slurm_node_info`

**Description:** Provides detailed information about SLURM nodes

**Labels:**
- `node_name`: Name of the SLURM node
- `instance_id`: Kubernetes instance identifier
- `state_base`: Base node state (IDLE, ALLOCATED, DOWN, ERROR, MIXED, UNKNOWN)
- `state_is_drain`: Whether node is in drain state ("true"/"false")
- `state_is_maintenance`: Whether node is in maintenance state ("true"/"false")
- `state_is_reserved`: Whether node is in reserved state ("true"/"false")
- `address`: IP address of the node

**Example:**
```prometheus
slurm_node_info{node_name="worker-1",instance_id="instance-123",state_base="ALLOCATED",state_is_drain="false",state_is_maintenance="false",state_is_reserved="false",address="10.0.1.15"} 1
```

#### Counter `slurm_node_gpu_seconds_total`

**Description:** Total GPU seconds accumulated on SLURM nodes

**Labels:**
- `node_name`: Name of the SLURM node
- `state_base`: Base node state
- `state_is_drain`: Drain state flag
- `state_is_maintenance`: Maintenance state flag
- `state_is_reserved`: Reserved state flag

**Example:**
```prometheus
slurm_node_gpu_seconds_total{node_name="worker-1",state_base="ALLOCATED",state_is_drain="false",state_is_maintenance="false",state_is_reserved="false"} 3600.5
```

#### Counter `slurm_node_fails_total`

**Description:** Total number of node state transitions to failed states (DOWN/DRAIN)

**Labels:**
- `node_name`: Name of the SLURM node
- `state_base`: Base node state at time of failure
- `state_is_drain`: Drain state flag
- `state_is_maintenance`: Maintenance state flag
- `state_is_reserved`: Reserved state flag
- `reason`: Reason for the node failure

**Example:**
```prometheus
slurm_node_fails_total{node_name="worker-2",state_base="DOWN",state_is_drain="true",state_is_maintenance="false",state_is_reserved="false",reason="hardware_failure"} 1
```

### Job Metrics

#### Gauge `slurm_job_info`

**Description:** Detailed information about SLURM jobs

**Labels:**
- `job_id`: SLURM job identifier
- `job_state`: Current job state (PENDING, RUNNING, COMPLETED, FAILED, etc.)
- `job_state_reason`: Reason for current job state
- `slurm_partition`: SLURM partition name
- `job_name`: User-defined job name
- `user_name`: Username who submitted the job
- `user_id`: Numeric user ID who submitted the job
- `standard_error`: Path to stderr file
- `standard_output`: Path to stdout file
- `array_job_id`: Array job ID (if applicable)
- `array_task_id`: Array task ID (if applicable)
- `submit_time`: When the job was submitted (Unix timestamp seconds, empty if not available or zero)
- `start_time`: When the job started execution (Unix timestamp seconds, empty if not available or zero)
- `end_time`: When the job completed (Unix timestamp seconds, empty if not available or zero). 
  **Warning:** For non-terminal states like RUNNING, this may contain a future timestamp representing 
  the forecasted end time based on the job's time limit
- `finished_time`: When the job actually finished for terminal states only (Unix timestamp seconds, 
  empty for non-terminal states or if end_time is zero). Unlike `end_time`, this field only contains 
  actual completion times, never forecasted values

**Example:**
```prometheus
slurm_job_info{job_id="12345",job_state="RUNNING",job_state_reason="None",slurm_partition="gpu",job_name="training_job",user_name="researcher",user_id="1000",standard_error="/home/researcher/job.err",standard_output="/home/researcher/job.out",array_job_id="",array_task_id="",submit_time="1722697200",start_time="1722697230",end_time="",finished_time=""} 1
```

#### Gauge `slurm_node_job`

**Description:** Mapping between jobs and the nodes they're running on

**Labels:**
- `job_id`: SLURM job identifier
- `node_name`: Name of the node running the job

**Example:**
```prometheus
slurm_node_job{job_id="12345",node_name="worker-1"} 1
```

#### Gauge `slurm_job_duration_seconds`

**Description:** Job duration in seconds. For running jobs, this is the time elapsed since the job started.
For completed jobs, this is the total execution time.

**Labels:**
- `job_id`: SLURM job identifier

**Notes:**
- Only exported for jobs with a valid start time
- For non-terminal states (RUNNING, etc.): duration = current_time - start_time
- For terminal states (COMPLETED, FAILED, etc.): duration = end_time - start_time (only if end_time is valid)

**Example:**
```prometheus
slurm_job_duration_seconds{job_id="12345"} 300.5
slurm_job_duration_seconds{job_id="12346"} 7200
```

### Controller RPC Metrics

These metrics provide insights into SLURM controller performance, similar to the output of the `sdiag` command, and were implemented to address [issue #1027](https://github.com/nebius/soperator/issues/1027).

#### Counter `slurm_controller_rpc_calls_total`

**Description:** Total count of RPC calls by message type

**Labels:**
- `message_type`: Type of RPC message (e.g., REQUEST_NODE_INFO, REQUEST_JOB_INFO, REQUEST_PING)

**Example:**
```prometheus
slurm_controller_rpc_calls_total{message_type="REQUEST_NODE_INFO"} 576
slurm_controller_rpc_calls_total{message_type="REQUEST_JOB_INFO"} 288
```

#### Counter `slurm_controller_rpc_duration_seconds_total`

**Description:** Total time spent processing RPCs by message type (converted from microseconds)

**Labels:**
- `message_type`: Type of RPC message

**Example:**
```prometheus
slurm_controller_rpc_duration_seconds_total{message_type="REQUEST_NODE_INFO"} 0.061410
slurm_controller_rpc_duration_seconds_total{message_type="REQUEST_JOB_INFO"} 0.030218
```

#### Counter `slurm_controller_rpc_user_calls_total`

**Description:** Total count of RPC calls by user

**Labels:**
- `user`: Username making the RPC calls
- `user_id`: Numeric user ID

**Example:**
```prometheus
slurm_controller_rpc_user_calls_total{user="root",user_id="0"} 2423
slurm_controller_rpc_user_calls_total{user="researcher",user_id="1000"} 100
```

#### Counter `slurm_controller_rpc_user_duration_seconds_total`

**Description:** Total time spent on user RPCs (converted from microseconds)

**Labels:**
- `user`: Username making the RPC calls
- `user_id`: Numeric user ID

**Example:**
```prometheus
slurm_controller_rpc_user_duration_seconds_total{user="root",user_id="0"} 0.172774
slurm_controller_rpc_user_duration_seconds_total{user="researcher",user_id="1000"} 0.005
```

#### Gauge `slurm_controller_server_thread_count`

**Description:** Number of server threads in the SLURM controller

**Example:**
```prometheus
slurm_controller_server_thread_count 1
```

### Self-Monitoring Metrics (Port 8081)

The exporter provides self-monitoring metrics to track its own health and performance.
These metrics are available on a separate endpoint (default port 8081) to avoid mixing operational metrics with business metrics.

#### Gauge `slurm_exporter_collection_duration_seconds`

**Description:** Duration of the most recent metrics collection from SLURM APIs

**Example:**
```prometheus
slurm_exporter_collection_duration_seconds 0.34
```

#### Counter `slurm_exporter_collection_attempts_total`

**Description:** Total number of metrics collection attempts

**Example:**
```prometheus
slurm_exporter_collection_attempts_total 150
```

#### Counter `slurm_exporter_collection_failures_total`

**Description:** Total number of failed metrics collection attempts

**Example:**
```prometheus
slurm_exporter_collection_failures_total 3
```

#### Counter `slurm_exporter_metrics_requests_total`

**Description:** Total number of requests to the `/metrics` endpoint

**Example:**
```prometheus
slurm_exporter_metrics_requests_total 245
```

#### Gauge `slurm_exporter_metrics_exported`

**Description:** Number of metrics exported in the last scrape

**Example:**
```prometheus
slurm_exporter_metrics_exported 127
```

### Accessing Self-Monitoring Metrics

To access self-monitoring metrics:

```bash
# Default monitoring port
curl http://localhost:8081/metrics

# Or with custom monitoring address
./soperator-exporter --monitoring-bind-address=:9090
curl http://localhost:9090/metrics
```

## Grafana Dashboard Example

The SLURM Exporter integrates with existing Grafana dashboards. Here's an example based on the production dashboard from [nebius-solutions-library](https://github.com/nebius/nebius-solutions-library/blob/release/soperator/soperator/modules/monitoring/templates/dashboards/cluster_health.json).

