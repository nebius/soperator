########################################################################################################################
# KUBERNETES CLUSTER CONFIGURATION
########################################################################################################################

## BASIC CONFIGURATION

### Nebius folder ID to create the K8S cluster in it.
k8s_folder_id = "bje82q7sm8njm3c4rrlq"

### Name of the K8S cluster. A short random prefix is added. For example, if you set "slurm-test" here, the actual name
### will be "slurm-test-randstr1.
k8s_cluster_name = "slurm"

### Description of the K8S cluster.
k8s_cluster_description = "K8S cluster for Slurm"

### Version of the K8S used in the cluster.
k8s_cluster_version = "1.28"

### Availability zone of the K8S cluster.
k8s_cluster_zone_id = "eu-north1-c"

### K8S cluster maintenance windows. During the specified intervals the K8S master may not be available.
k8s_cluster_master_maintenance_windows = [{
  day        = "monday"
  start_time = "20:00"
  duration   = "3h"
}]



## NETWORK CONFIGURATION

### ID of an existing network in which a new subnet for the K8S cluster is created. If empty, a new network is created.
### A separate subnet is created in either case.
#k8s_network_id = "TODO: Put your network ID here"

### IPv4 CIDR blocks for the new subnet. In case the subnet is created in an existing network, ensure it doesn't
### conflict with CIDR blocks of existing subnets.
k8s_cluster_subnet_cidr_blocks = ["192.168.10.0/24"]



## NODE GROUP CONFIGURATION
## ---------------------------------------------------------------------------------------------------------------------
## This terraform creates a Slurm cluster with two node groups:
## - one with GPUs onboard for running compute workloads (Slurm worker nodes),
## - and one without GPUs for running undemanding workloads (Slurm login & controller nodes, system K8S jobs, etc.).

### Configuration of the node group with GPUs. Its nodes are interconnected and forms a GPU cluster.
k8s_cluster_node_group_gpu = {
  #### The kind of GPUs. For example, "h100" (type A), "h100-c" (type C), "h100-c-llm" (type C allowing "preemptible").
  platform = "h100-a-llm"

  #### Whether the nodes can be taken away in favor of higher priority tasks. The only allowed platform is "h100-c-llm".
  preemptible = true

  #### Number of nodes in the group. It should be at least 2 in order to benefit from the GPU cluster interconnection.
  #### The created node group doesn't have auto-scaling, but the size can be updated using this terraform.
  size = 2

  #### Number of vCPU on the nodes. Not any value is supported. Typically, each GPU platform has only a single permitted
  #### set of resources (CPU & memory).
  cpu_cores = 160

  #### Size of the real memory on the nodes in GB. Not any value is supported. See the comment above.
  memory_gb = 1280

  #### Number of GPUs on each node.
  gpus = 8

  #### Interconnect type. Typically, "InfiniBand".
  interconnect_type = "InfiniBand"

  #### Interconnect physical cluster name. GPUs of certain platforms can be created only in certain physical clusters.
  #### e.g. "h100" platform can be created only in "fabric-1", and "h100-c" & "h100-c-llm" in "fabric-4" or fabric-6".
  #### This value cannot be changed after creation.
  interconnect_physical_cluster = "fabric-1"

  #### Type of boot disks attached to the nodes.
  disk_type = "network-ssd"

  #### Size of boot disks in GB.
  disk_size_gb = 1024

  #### Value for the "cloud.google.com/gke-accelerator" label assigned to each node. Should be "nvidia-h100-80gb" for
  #### all H100 GPU platforms.
  gke_accelerator = "nvidia-h100-80gb"

  #### Major version of the NVIDIA GPU driver to be installed on the nodes.
  driver_config = "535"
}

### Configuration of the node group without GPUs.
k8s_cluster_node_group_non_gpu = {
  #### Number of nodes in the group. It should be at least 2 in order to benefit from K8S high-availability features.
  #### The created node group doesn't have auto-scaling, but the size can be updated using this terraform.
  size = 2

  #### Number of vCPU on the nodes with platform "standard-v2". Not any value is supported. The platform has only
  #### specific permitted sets of resources (CPU & memory). For example, 8 CPU & 32 memory, or 32 CPU & 128 memory.
  cpu_cores = 32

  #### Size of the real memory on the nodes in GB. Not any value is supported. See the comment above.
  memory_gb = 128

  #### Type of boot disks attached to the nodes.
  disk_type = "network-ssd"

  #### Size of boot disks in GB.
  disk_size_gb = 1024
}



## SSH CONFIGURATION

### Username for connecting to K8S nodes.
k8s_cluster_ssh_username = "ubuntu"

### SSH public key for connecting to K8S nodes. Either the key as a string or path to the key must be set.
k8s_cluster_ssh_public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICL8scMKnwu+Y9S6XDACacZ54+qu+YRo2y4IeXSPjTuo pavel@sofrony.ru"
k8s_cluster_ssh_public_key_path = null



## NVIDIA NETWORK OPERATOR CONFIGURATION

### Version of the network operator installed to the K8S cluster.
k8s_cluster_operator_network_version = "23.7.0"



## NVIDIA GPU OPERATOR CONFIGURATION

### Version of the GPU operator installed to the K8S cluster.
k8s_cluster_operator_gpu_version = "v23.9.0"

### NVIDIA GPU driver version. The major version must match with k8s_cluster_node_group_gpu.driver_config variable.
k8s_cluster_operator_gpu_driver_version = "535.104.12"

### Whether to use nvidia-container-toolkit for propagating NVIDIA drivers and system libraries from K8S nodes to
### containers. Typically, must be "true".
k8s_cluster_operator_gpu_cuda_toolkit = true

# Whether to enable GPU driver RDMA. Typically, must be "true".
k8s_cluster_operator_gpu_driver_rdma = true





########################################################################################################################
# SHARED STORAGE CONFIGURATION
# ----------------------------------------------------------------------------------------------------------------------
# At least two shared storages are created for the Slurm cluster:
# 1. "Jail" storage that keeps the root directory of the shared environment within which users interact with Slurm.
#    Can be either a compute file storage or a GlusterFS.
# 2. "Controller spool" storage that keeps the state of Slurm controller (the Slurm's "StateSaveLocation"). This state
#    is shared between the primary and backup controllers.
# In addition, an arbitrary number of "jail submount" storages can be created. These storages are mounted into the jail
# environment at specified paths. For example, you can mount /home directory from a different storage, or have separate
# storages with datasets or checkpoints.
# All jail submounts are compute file storages.
########################################################################################################################

## BASIC COMPUTE FILE STORAGE CONFIGURATION
## ---------------------------------------------------------------------------------------------------------------------
## Always applies to "controller spool" and all "jail submount" storages. Applies to the "jail" storage only if
## "slurm_cluster_storages.jail.type" variable equals to "filestore".

### Block size for all used compute file storages in bytes.
k8s_cluster_filestore_block_size = 32768



## BASIC GLUSTER FS STORAGE CONFIGURATION
## ---------------------------------------------------------------------------------------------------------------------
## Applies to the "jail" storage if "slurm_cluster_storages.jail.type" variable equals to "glusterfs". Otherwise, these
## settings do nothing.

### Folder ID to create GlusterFS nodes in it. Several GlusterFS storages should not be created in the same folder due
### to possible conflicts in compute instance names.
glusterfs_cluster_folder_id = "bje82q7sm8njm3c4rrlq"

### SSH key for connecting to GlusterFS compute instances.
glusterfs_cluster_ssh_public_key = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICL8scMKnwu+Y9S6XDACacZ54+qu+YRo2y4IeXSPjTuo pavel@sofrony.ru"
glusterfs_cluster_ssh_public_key_path = null

### Size of separate disks comprising the cluster in GB. The total size of the storage = (disk size * number of nodes).
### For example, if 3 disks are used, each of 372 GB, the total size of the storage is 372 * 3 = 1116 GB.
### Must be a multiple of 93 GB.
glusterfs_cluster_disk_size = 372

### Number of nodes in the cluster.
glusterfs_cluster_nodes = 3

### Number of disks on each node.
glusterfs_cluster_disks_per_node = 1



## SLURM CLUSTER STORAGE CONFIGURATION
## ---------------------------------------------------------------------------------------------------------------------
## Configures how Slurm storages are represented in K8S.

### Configuration of the shared storages mounted to Slurm nodes.
slurm_cluster_storages = {
  #### "Jail" storage configuration.
  jail = {
    ##### Name of the storage. It doesn't matter a lot. Used as a base name for different entities: compute file storage
    ##### name, mounted device name, K8S PV & PVC names, and the like.
    name = "jail"

    ##### Size of the storage in bytes. In case GlusterFS is used, it must not exceed the total size of the storage set
    ##### in the "glusterfs_cluster_disk_dize" and "glusterfs_cluster_nodes" variables.
    size = 1115 * (1024 * 1024 * 1024) # 1115Gi

    ##### Type of the shared storage. Can be either "glusterfs" or "filestore".
    type = "glusterfs"

    ##### ID for an existing compute file storage for using it instead of creating a new one. It is relevant, only when
    ##### type = "filestore".
    filestore_id = null
  }

  #### "Controller spool" storage configuration.
  controller_spool = {
    ##### Name of the storage. It doesn't matter a lot. Used as a base name for different entities: compute file storage
    ##### name, mounted device name, K8S PV & PVC names, and the like.
    name = "controller-spool"

    ##### Size of the storage in bytes.
    size = 100 * (1024 * 1024 * 1024) # 100Gi

    ##### ID for an existing compute file storage for using it instead of creating a new one.
    filestore_id = null
  }

  #### "Jail submount" storages configuration. If empty, no additional shared storages are mounted to the jail.
  #### All these storages are initially mounted with 777 permissions and root:root ownerships, but users can change them
  #### after the Slurm cluster is created.
  #### It's enough to execute the command like `sudo chmod 755 /datasets && sudo chown bob:bob /datasets` on any of
  #### the Slurm nodes (login or worker) and these changes will apply to all other nodes in the cluster.
  jail_submounts = [{
    ##### Name of the storage. It doesn't matter a lot. Used as a base name for different entities: compute file storage
    ##### name, mounted device name, K8S PV & PVC names, and the like.
    name = "mlperf-sd"

    ##### Size of the storage in bytes.
    size = 1500 * (1024 * 1024 * 1024) # 1500Gi

    ##### The absolute path within the jail environment for which data will be available to users.
    mountPath = "/mlperf-sd"

    ##### ID for an existing compute file storage for using it instead of creating a new one.
    filestore_id = "dp7fkrn4ssh5adhbbk87"
  }]
}

### Configuration of PVC with the initial jail content. Must be an object with fields name and size (in bytes). If set,
### this PVC is used during the initial cluster creation to populate the "jail" shared storage with its content.
### See the comment to the "slurm_cluster_create_cr" variable for details.
slurm_cluster_jail_snapshot = null

### Size of the directory storing the slurmd state, that is node-local for each worker.
slurm_cluster_worker_volume_spool_size = 128 * (1024 * 1024 * 1024) # 128Gi





########################################################################################################################
# SLURM CONFIGURATION
########################################################################################################################

## SLURM OPERATOR CONFIGURATION

### Version of the Slurm operator. Typically, should be left default.
slurm_operator_version = "1.1.0"



## BASIC SLURM CLUSTER CONFIGURATION

### Whether to create a Slurm cluster within this terraform. If false, only the operator is created, without a cluster.
### This may be useful in scenario when a custom initial content for the "jail" shared storage is needed. It may be
### achieved by the following steps:
### 1. Apply the terraform with "slurm_cluster_create_cr = false"
### 2. Manually create a PVC in the K8S cluster with the content you want to have in the jail environment.
### 3. Apply the terraform again with "slurm_cluster_create_cr = true" and "slurm_cluster_jail_snapshot.name" set to
###    your PVC name.
slurm_cluster_create_cr = true

### Name of the Slurm cluster.
slurm_cluster_name = "slurm-dev"

### List of SSH public keys that will authorized for user root. After connecting to the cluster as root, the Slurm admin
### can create other Linux users with different authorized SSH keys.
slurm_cluster_ssh_root_public_keys = [
  "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIKzxkjzPQ4EyZSjan4MLGFSA18idpZicoKW7HC4YmwgN rdjjke@gmail.com",
  "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAICL8scMKnwu+Y9S6XDACacZ54+qu+YRo2y4IeXSPjTuo pavel@sofrony.ru",
  "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQDFD3Lzq7EVKshkUl5P8Yay+V22L86W2cQ1Z5kJnE4UfQAYfHUC6XhW0Gq2hDSVFvgYB2H0sU38vc13L8ZmgTIvgfoz2D+7/sf7HmFyQWm5f/eZa+koBiyu1UUjPk7Q65bCvn0iXguXfnOQywxY/uvmVlRDfkZmRs6OfDuEjPd2HH36yh86RzqfUVFePVl3kylKoNpXPiUHaGKYtatqqJ72dn4iPC3p2WCqVd9bmIahixJsBvEKMbjcwkYbYsW14GAKRMjtrOaF8pGctXKSWNRO9t6kzFCy418rK9Uig1EdDXIXyLyYO6IJadteIDo2rDuiQE64hrogOdczIL70XNAOd/1/C9Mv4tTKXUbyMpJmzo8LP5D/AADmbUjVVLPUQR2rEl2aAd6DFGips8x9Qv903aflJTDr682+oVK0QgljKzkYNXJwAWWIz3wY21oFnyhnRKl7aT1RaB3GdmVJ8Gzf1qVuPAQhDICMndO03fM3DUx0RwlVIawsX2hQAjmBPeM= dstaroff@dstaroff-osx"
]



## WORKER NODES CONFIGURATION
## ---------------------------------------------------------------------------------------------------------------------
## Slurm worker nodes are running on K8S nodes with GPU.
## Keep in mind, that not all compute instance resources are available for allocation in containers. The typical K8S
## overhead on each node is 2 vCPU, 4 GiB of memory and 50 GiB of the local disk. And it's better to leave a bit more.

### Number of worker nodes where slurmd daemon runs. Slurm jobs will be executed on these nodes.
slurm_cluster_node_worker_count = 2

### Resources, dedicated to the slurmd daemon on each worker node. If set to null, the container will have no K8S
### resource requests & limits.
### The slurmd container is the place where all user jobs are executed. It must have most of the resources of GPU K8S
### nodes.
slurm_cluster_node_worker_slurmd_resources = {
  cpu_cores               = 156
  memory_bytes            = 1220 * (1024 * 1024 * 1024) # 1220Gi
  ephemeral_storage_bytes = 720 * (1024 * 1024 * 1024) # 720Gi
}

### Resources, dedicated to the munged daemon on each worker node. If set to null, the container has will have no K8S
### resource requests & limits.
### The munged container is used for internal authentication within the Slurm cluster.
slurm_cluster_node_worker_munge_resources = {
  cpu_cores               = 2
  memory_bytes            = 4 * (1024 * 1024 * 1024) # 4Gi
  ephemeral_storage_bytes = 8 * (1024 * 1024 * 1024) # 8Gi
}



## CONTROLLER NODES CONFIGURATION
## ---------------------------------------------------------------------------------------------------------------------
## Slurm controller nodes are running on K8S nodes without GPU.
## Keep in mind, that not all compute instance resources are available for allocation in containers. The typical K8S
## overhead on each node is 2 vCPU, 4 GiB of memory and 50 GiB of the local disk. And it's better to leave a bit more.
## Controller & login nodes together should also not allocate all node resources to the fullest, because some space
## is needed for the system workload (such as GPU benchmark starter jobs).

### Number of controller nodes where the slurmctld daemon runs. The first available controller is primary, and others
### are backup ones. When the current primary controller goes down, the first available backup controller takes control.
### There is little sense in setting it to more than 3.
slurm_cluster_node_controller_count = 2

### Resources, dedicated to the slurmctld daemon on each controller node. If set to null, the container will have no K8S
### resource requests & limits.
### The slurmdctld container is the place where the Slurm cluster is controlled from. It must have enough resources for
### operation, but there's no sense in giving it more than 16 CPU and 64 GiB of memory.
slurm_cluster_node_controller_slurmctld_resources = {
  cpu_cores               = 8
  memory_bytes            = 32 * (1024 * 1024 * 1024) # 32Gi
  ephemeral_storage_bytes = 16 * (1024 * 1024 * 1024) # 16Gi
}

### Resources, dedicated to the munged daemon on each controller node. If set to null, the container will have no K8S
### resource requests & limits.
### The munged container is used for internal authentication within the Slurm cluster.
slurm_cluster_node_controller_munge_resources = {
  cpu_cores               = 1
  memory_bytes            = 2 * (1024 * 1024 * 1024) # 2Gi
  ephemeral_storage_bytes = 4 * (1024 * 1024 * 1024) # 4Gi
}



## LOGIN NODES CONFIGURATION
## ---------------------------------------------------------------------------------------------------------------------
## Slurm login nodes are running on K8S nodes without GPU.
## Keep in mind, that not all compute instance resources are available for allocation in login nodes. The typical K8S
## overhead on each node is 2 vCPU, 4 GiB of memory and 50 GiB of the local disk. And it's better to leave a bit more.
## Controller & login nodes together should also not allocate all node resources to the fullest, because some space
## is needed for the system workload (such as GPU benchmark starter jobs).

### Number of login nodes where the sshd daemon runs. When a user connects to the Slurm cluster by SSH, they are
### directed to a random node. Setting this value to more than 1 makes sense only for high availability or for
### distributing user sessions across several computationally weak nodes.
slurm_cluster_node_login_count = 2

### Resources, dedicated to the sshd daemon on each login node. If set to null, the container will have no K8S resource
### requests & limits.
### The sshd container is the place where the Slurm users are connected to. It must have as many resources, as clients
### need, but typically not so many because they are used as a thin client.
slurm_cluster_node_login_sshd_resources = {
  cpu_cores               = 16
  memory_bytes            = 64 * (1024 * 1024 * 1024) # 64Gi
  ephemeral_storage_bytes = 720 * (1024 * 1024 * 1024) # 720Gi
}

### Resources, dedicated to the munged daemon on each login node. If set to null, the container will have no K8S
### resource requests & limits.
### The munged container is used for internal authentication within the Slurm cluster.
slurm_cluster_node_login_munge_resources = {
  cpu_cores               = 1
  memory_bytes            = 2 * (1024 * 1024 * 1024) # 2Gi
  ephemeral_storage_bytes = 4 * (1024 * 1024 * 1024) # 4Gi
}



## PERIODIC GPU BENCHMARK CONFIGURATION
## ---------------------------------------------------------------------------------------------------------------------
## GPU benchmarks are implemented as "all_reduce_perf" NCCL tests. These checks are launched as usual Slurm jobs, which
## are periodically scheduled from a K8S CronJob and stay in the same queue with users' workload.

### Cron string representing the schedule for running NCCL benchmarks.
slurm_cluster_nccl_benchmark_schedule = "0 */3 * * *" # Every 3 hours

### Configuration of NCCL benchmarks. Most of the parameters are just passed as arguments to all_reduce_perf NCCL test.
slurm_cluster_nccl_benchmark_settings = {
  #### The initial size of the data that is transferred at the beginning of the benchmark.
  min_bytes = "512Mb"

  #### The final size of the data that is transferred at the end of the benchmark.
  max_bytes = "8Gb"

  #### Step factor affecting the number of data transfers.
  step_factor = "2"

  ### Benchmark timeout. If the benchmark exceeds it, the Slurm nodes won't be drained, but the K8S job becomes Failed.
  timeout = "20:00"

  ### The target value of the "Avg bus bandwidth" result in "all_reduce_perf" NCCL test. If some Slurm worker node shows
  ### a result less that this threshold, it is drained.
  threshold_more_than = "420"

  ### Whether to use Infiniband (true) or NVLink (false) during this test. Changing this variable may require changing
  ### the "threshold_more_than" variable as well. The normal limit for Infiniband is 42 and for NVLink is 420.
  use_infiniband = false
}

### Whether to drain Slurm nodes that showed unsatisfactory results. When drained, the details of the benchmark results
### are set to the Slurm node Reason field. Users can see it by executing `scontrol show nodes`
slurm_cluster_nccl_benchmark_drain_nodes = true
