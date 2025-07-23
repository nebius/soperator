# SLURM Exporter

## Overview

The SLURM Exporter is a component of Soperator that collects metrics from SLURM clusters and exports them in Prometheus format. It provides comprehensive monitoring capabilities for SLURM cluster health, job status, node states, and controller performance metrics.

The exporter integrates seamlessly with the Prometheus monitoring stack and enables observability for SLURM workloads running on Kubernetes through Soperator.

### Key Features

- **Real-time monitoring** of SLURM nodes, jobs, and controller performance
- **Prometheus-native metrics** with standardized naming conventions
- **Rich labeling** for detailed filtering and aggregation
- **Controller RPC diagnostics** similar to SLURM's `sdiag` command
- **Kubernetes-native deployment** as part of Soperator


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
- `submit_time`: When the job was submitted (Unix timestamp seconds, empty if not available)
- `start_time`: When the job started execution (Unix timestamp seconds, empty if not available)
- `end_time`: When the job completed (Unix timestamp seconds, empty if not available)

**Example:**
```prometheus
slurm_job_info{job_id="12345",job_state="RUNNING",job_state_reason="None",slurm_partition="gpu",job_name="training_job",user_name="researcher",user_id="1000",standard_error="/home/researcher/job.err",standard_output="/home/researcher/job.out",array_job_id="",array_task_id="",submit_time="1722697200",start_time="1722697230",end_time=""} 1
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

## Grafana Dashboard Example

The SLURM Exporter integrates with existing Grafana dashboards. Here's an example based on the production dashboard from [nebius-solutions-library](https://github.com/nebius/nebius-solutions-library/blob/release/soperator/soperator/modules/monitoring/templates/dashboards/cluster_health.json).

