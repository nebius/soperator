# Features
This document contains a list of all the features that this solution brings in comparison with a typical Slurm setup.

It is divided into 2 sections: what Slurm functionality is currently supported, and unique features that can't present
in typical installations.



## Supported Slurm Functionality


### NVIDIA GPU Model Auto-Detection
The operator automatically detects NVIDIA GPU models and register the corresponding GRES including with types. For 
example: `gpu:nvidia_h100_80gb_hbm3:8`.


### Dynamic Nodes
Worker nodes register themselves in the controller upon start.


### Cgroups
Cgroups V2 are used for limiting access of jobs to resources on a node. All available cgroups are enabled except for the 
swap space.


### Job Containers
This solution supports (and pre-installs) Pyxis+enroot containerization plugin. Pulled images are stored on the "jail" 
shared storage so users don't need to separately pull it on each node. Container filesystems are stored either on tmpfs 
or node-local volumes.


### Client Commands Auto-Completion
Auto-completion for all client commands is enabled in the jail container image.



## Unique Features


### Shared Root Filesystem
When users interact with a Slurm cluster they see a shared file system as their root "**/**" directory. This approach
allowed us to retain the familiar way of using Slurm (e.g. users don't have to run all jobs in containers).

It also frees users from maintaining nodes in an identical state. They can, on one node, install new software packages,
create new Linux users, write job outputs, or download datasets, and **instantly get the changes on all other nodes**.
This shared filesystem is called *"jail"*.

This functionality is implemented as a SPANK plugin that is executed after forking `slurmd` but before launching the job
command. The plugin creates & enters new mount namespace and changes the root directory of the process.

In fact, not the whole root filesystem that users' job see is stored on the shared storage. Some of the directories are 
node-local. This is sorted out in detail in the architecture page.

Users can also mount additional storages of various kinds (shared, node-local & persistent, in-memory, even OCI - 
anything K8s supports) into subdirectories of the jail storage.


### GPU Health Checks
This is only applicable to NVIDIA GPUs.

The operator performs GPU health checks periodically. If any Slurm node shows an unsatisfactory result, the operator
“drains” it, which excludes the node from scheduling new jobs on it.

There are two checks at the moment:
1. **NCCL tests**. Soperator creates a K8s CronJob that schedules a normal job in Slurm. This job performs the 
`all_reduce_perf` NCCL test and depending on the results either finish successfully, or draining the Slurm node where it
was executed.
2. **Nvidia-smi**. We use the Slurm's `HealthCheckProgram` parameter to execute `nvidia-smi` on each node every 30 sec.
If it completes with a non-zero exit code, the node is drained as well.

When a node is drained Slurm allows already executing jobs to finish, but excludes the node from scheduling other jobs.


### Easy Scaling
This solution allows Slurm to reuse the unique Kubernetes' ability to scale automatically depending on the current
needs. You can simply change a single value in the YAML manifest, and watch the cluster changes in size.

Node groups of each type (Worker, Login, and Controller) can be changed independently and on the fly.


### High Availability
Kubernetes brings some level of HA out of the box. If a Pod or container dies (e.g. Slurm controller), Kubernetes
recreates it.

Our operator improves this further, continuously bringing the entire cluster to the desired state.


### Isolation of User Actions
Users can’t unintentionally break the Slurm cluster itself - all their actions are isolated within a dedicated
environment (some sort of container). This clearly defines the boundary between the operator's responsibility and the
users' one.

User's actions are currently isolated only on the FS layer: users can't see and access files used by Slurm daemons, but
they can see the processes and send signals for example. They also have superuser privileges on the host environment.

Improving isolation further is in our todo list.


### Observability
This solution implements integration with a monitoring stack that can be installed separately (though we provide Helm
charts for that). It consists of gathering various Slurm statistics and hardware utilization metrics. Users can observe
this information on the dashboards we provide as well.

At the moment, the following information is gathered and can be viewed by users:
- Logs for all K8s pods + K8s events.
- **Centralized Slurm workload logs**: Structured collection of job outputs with automatic categorization.
- K8s cluster metrics: node states, stateful set & deployment sizes, etc.
- K8s node metrics: CPU, memory, network, disk usage, etc.
- K8s pod resource metrics: resource usage by pods & containers.
- NVIDIA GPU metrics: GPU utilization, power, temperature, etc.
- Slurm metrics: comprehensive monitoring of nodes, jobs, and controller performance. See [SLURM Exporter](slurm-exporter.md) for detailed metrics documentation.


### Centralized Logging Scheme

Soperator implements a centralized logging system that automatically collects and categorizes Slurm workload outputs. Logs are stored in a structured directory layout and processed by OpenTelemetry collectors for centralized analysis.

#### Directory Structure
```
/opt/soperator-outputs/
├── nccl_logs/      # NCCL benchmark outputs
├── slurm_jobs/     # Slurm job outputs
└── slurm_scripts/  # Slurm script outputs
```

#### Logging Schema

Log files follow specific naming patterns for automatic parsing and labeling:

**NCCL Logs:**
```
worker_name.job_id.job_step_id.out
Example: worker-0.12345.67890.out
```

**Slurm Jobs:**
```
worker_name.job_name.job_id[.array_id].out
Examples:
- worker-0.benchmark.12345.out
- worker-0.training.12345.1.out (array job)
```

**Slurm Scripts:**
```
worker_name.script_name[.context].out
Examples:
- worker-0.setup.out
- worker-0.cleanup.batch.out
```

#### Generated Labels

The logging system automatically extracts metadata and creates the following labels:

- `worker_name`: Worker pod identifier (e.g., worker-0)
- `log_type`: Category (nccl_logs, slurm_jobs, slurm_scripts)
- `job_id`, `job_step_id`: For NCCL logs
- `job_name`, `job_array_id`: For Slurm job logs
- `slurm_script_name`, `slurm_script_context`: For script logs

