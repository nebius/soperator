---
title: "Getting started with {{ slurm-operator }}"
description: "In this tutorial, you will learn how to create a Slurm cluster in Kubernetes with {{ slurm-operator }}."
---

# Getting started with {{ slurm-operator }}

## Overview
This solution allows you to create a Slurm cluster in Kubernetes with a single terraform apply.

Running Slurm in Kubernetes using this operator brings several features and possibilities.

#### Easy scaling
You can scale the Slurm cluster up or down without the need to bootstrap new nodes from scratch.

#### High availability
K8S provides some self-healing out of the box: Slurm nodes represented as K8S pods are automatically restarted in case 
of problems.

#### Shared root filesystem
When users interact with Slurm, they see a single shared persistent storage as the root directory on each Slurm node. 
This frees users from the Slurm requirement that is very difficult to achieve: all nodes must be identical. Because of
the storage, users don't need to manually synchronise all software versions and Linux UIDs & GIDs among the nodes.

#### Protection against accidental Slurm breakage
Users connect to login nodes and execute jobs on worker nodes not on the system where Slurm daemons are running, but in 
a special isolated environment from which it's almost impossible to accidentally break Slurm. In addition, GPU drivers &
libraries are mounted from K8S nodes so users can't irreversibly break them.

#### Periodic GPU health checks
NCCL tests are periodically launched on all Slurm workers, and nodes that show unsatisfactory results are drained. These
checks are implemented as usual Slurm jobs - they stay in the same queue with users' workload and doesn't interfere it.



## How to create a Slurm cluster
The creation process consists of the following steps.

#### Step 1. Download a release of the Slurm operator
Download tarball with the latest version from [Arcanum](https://arcanum.nebius.dev/nebo/msp/slurm-service/internal/operator/terraform-releases/stable).
In browser, press on the file with the latest version, and then press the "Raw" button in the upper right corner.

#### Step 2. Extract the tarball
Create and open a directory: `mkdir slurm-operator-tf`

Unpack the tarball into it: `tar -xvf slurm_operator_tf_*****.tar.gz -C ./slurm-operator-tf`

#### Step 3. Initialize terraform
Enter the directory with terraform files: `cd terraform/oldbius`

Execute `terraform init`. This command will download all referenced modules.

#### Step 4. Fill out terraform variables
Put your values to the `terraform.tfvars` file. Use the `terraform.tfvars.example` as a reference. All variables there 
are comprehensively commented, and you'll probably leave most of them with preset values.
Required variables (e.g. a folder ID or your public SSH key) have values prefixed with "TODO".

<details>
  <summary>Example of terraform configuration</summary>

> **Disclaimer**: This is just an example. For actual information, refer the `terraform.tfvars.example` file from your 
> release tarball.
> ```terraform
> ########################################################################################################################
> # KUBERNETES CLUSTER CONFIGURATION
> ########################################################################################################################
> 
> ## BASIC CONFIGURATION
> 
> ### Nebius folder ID to create the K8S cluster in it.
> k8s_folder_id = "TODO: Put your folder ID here"
> 
> ### Name of the K8S cluster. A short random prefix is added. For example, if you set "slurm-test" here, the actual name
> ### will be "slurm-test-randstr1.
> k8s_cluster_name = "slurm"
> 
> ### Description of the K8S cluster.
> k8s_cluster_description = "K8S cluster for Slurm"
> 
> ### Version of the K8S used in the cluster.
> k8s_cluster_version = "1.28"
> 
> ### Availability zone of the K8S cluster.
> k8s_cluster_zone_id = "eu-north1-c"
> 
> ### K8S cluster maintenance windows. During the specified intervals the K8S master may not be available.
> k8s_cluster_master_maintenance_windows = [{
>     day        = "monday"
>     start_time = "20:00"
>     duration   = "3h"
> }]
> 
> 
> 
> ## NETWORK CONFIGURATION
> 
> ### ID of an existing network in which a new subnet for the K8S cluster is created. If empty, a new network is created.
> ### A separate subnet is created in either case.
> k8s_network_id = ""
> 
> ### IPv4 CIDR blocks for the new subnet. In case the subnet is created in an existing network, ensure it doesn't
> ### conflict with CIDR blocks of existing subnets.
> k8s_cluster_subnet_cidr_blocks = ["192.168.10.0/24"]
> 
> 
> 
> ## NODE GROUP CONFIGURATION
> ## ---------------------------------------------------------------------------------------------------------------------
> ## This terraform creates a Slurm cluster with two node groups:
> ## - one with GPUs onboard for running compute workloads (Slurm worker nodes),
> ## - and one without GPUs for running undemanding workloads (Slurm login & controller nodes, system K8S jobs, etc.).
> 
> ### Configuration of the node group with GPUs. Its nodes are interconnected and forms a GPU cluster.
> k8s_cluster_node_group_gpu = {
>     #### The kind of GPUs. For example, "h100" (type A), "h100-c" (type C), "h100-c-llm" (type C allowing "preemptible").
>     platform = "h100"
>     
>     #### Whether the nodes can be taken away in favor of higher priority tasks. The only allowed platform is "h100-c-llm".
>     preemptible = false
>     
>     #### Number of nodes in the group. It should be at least 2 in order to benefit from the GPU cluster interconnection.
>     #### The created node group doesn't have auto-scaling, but the size can be updated using this terraform.
>     size = 2
>     
>     #### Number of vCPU on the nodes. Not any value is supported. Typically, each GPU platform has only a single permitted
>     #### set of resources (CPU & memory).
>     cpu_cores = 160
>     
>     #### Size of the real memory on the nodes in GB. Not any value is supported. See the comment above.
>     memory_gb = 1280
>     
>     #### Number of GPUs on each node.
>     gpus = 8
>     
>     #### Interconnect type. Typically, "InfiniBand".
>     interconnect_type = "InfiniBand"
>     
>     #### Interconnect physical cluster name. GPUs of certain platforms can be created only in certain physical clusters.
>     #### e.g. "h100" platform can be created only in "fabric-1", and "h100-c" & "h100-c-llm" in "fabric-4" or fabric-6".
>     #### This value cannot be changed after creation.
>     interconnect_physical_cluster = "fabric-1"
>     
>     #### Type of boot disks attached to the nodes.
>     disk_type = "network-ssd"
>     
>     #### Size of boot disks in GB.
>     disk_size_gb = 1024
>     
>     #### Value for the "cloud.google.com/gke-accelerator" label assigned to each node. Should be "nvidia-h100-80gb" for
>     #### all H100 GPU platforms.
>     gke_accelerator = "nvidia-h100-80gb"
>     
>     #### Major version of the NVIDIA GPU driver to be installed on the nodes.
>     driver_config = "535"
> }
> 
> ### Configuration of the node group without GPUs.
> k8s_cluster_node_group_non_gpu = {
>     #### Number of nodes in the group. It should be at least 2 in order to benefit from K8S high-availability features.
>     #### The created node group doesn't have auto-scaling, but the size can be updated using this terraform.
>     size = 2
>     
>     #### Number of vCPU on the nodes with platform "standard-v2". Not any value is supported. The platform has only
>     #### specific permitted sets of resources (CPU & memory). For example, 8 CPU & 32 memory, or 32 CPU & 128 memory.
>     cpu_cores = 32
>     
>     #### Size of the real memory on the nodes in GB. Not any value is supported. See the comment above.
>     memory_gb = 128
>     
>     #### Type of boot disks attached to the nodes.
>     disk_type = "network-ssd"
>     
>     #### Size of boot disks in GB.
>     disk_size_gb = 1024
> }
> 
> 
> 
> ## SSH CONFIGURATION
> 
> ### Username for connecting to K8S nodes.
> k8s_cluster_ssh_username = "ubuntu"
> 
> ### SSH public key for connecting to K8S nodes. Either the key as a string or path to the key must be set.
> k8s_cluster_ssh_public_key = "TODO: Put your public SSH key here"
> k8s_cluster_ssh_public_key_path = null
> 
> 
> 
> ## NVIDIA NETWORK OPERATOR CONFIGURATION
> 
> ### Version of the network operator installed to the K8S cluster.
> k8s_cluster_operator_network_version = "23.7.0"
> 
> 
> 
> ## NVIDIA GPU OPERATOR CONFIGURATION
> 
> ### Version of the GPU operator installed to the K8S cluster.
> k8s_cluster_operator_gpu_version = "v23.9.0"
> 
> ### NVIDIA GPU driver version. The major version must match with k8s_cluster_node_group_gpu.driver_config variable.
> k8s_cluster_operator_gpu_driver_version = "535.104.12"
> 
> ### Whether to use nvidia-container-toolkit for propagating NVIDIA drivers and system libraries from K8S nodes to
> ### containers. Typically, must be "true".
> k8s_cluster_operator_gpu_cuda_toolkit = true
> 
> # Whether to enable GPU driver RDMA. Typically, must be "true".
> k8s_cluster_operator_gpu_driver_rdma = true
> 
> 
> 
> 
> 
> ########################################################################################################################
> # SHARED STORAGE CONFIGURATION
> # ----------------------------------------------------------------------------------------------------------------------
> # At least two shared storages are created for the Slurm cluster:
> # 1. "Jail" storage that keeps the root directory of the shared environment within which users interact with Slurm.
> #    Can be either a compute file storage or a GlusterFS.
> # 2. "Controller spool" storage that keeps the state of Slurm controller (the Slurm's "StateSaveLocation"). This state
> #    is shared between the primary and backup controllers.
> # In addition, an arbitrary number of "jail submount" storages can be created. These storages are mounted into the jail
> # environment at specified paths. For example, you can mount /home directory from a different storage, or have separate
> # storages with datasets or checkpoints.
> # All jail submounts are compute file storages.
> ########################################################################################################################
> 
> ## BASIC COMPUTE FILE STORAGE CONFIGURATION
> ## ---------------------------------------------------------------------------------------------------------------------
> ## Always applies to "controller spool" and all "jail submount" storages. Applies to the "jail" storage only if
> ## "slurm_cluster_storages.jail.type" variable equals to "filestore".
> 
> ### Block size for all used compute file storages in bytes.
> k8s_cluster_filestore_block_size = 32768
> 
> 
> 
> ## BASIC GLUSTER FS STORAGE CONFIGURATION
> ## ---------------------------------------------------------------------------------------------------------------------
> ## Applies to the "jail" storage if "slurm_cluster_storages.jail.type" variable equals to "glusterfs". Otherwise, these
> ## settings do nothing.
> 
> ### Folder ID to create GlusterFS nodes in it. Several GlusterFS storages should not be created in the same folder due
> ### to possible conflicts in compute instance names.
> glusterfs_cluster_folder_id = "TODO: Put your folder ID here"
> 
> ### SSH key for connecting to GlusterFS compute instances.
> glusterfs_cluster_ssh_public_key = "TODO: Put your public SSH key here"
> glusterfs_cluster_ssh_public_key_path = null
> 
> ### Size of separate disks comprising the cluster in GB. The total size of the storage = (disk size * number of nodes).
> ### For example, if 3 disks are used, each of 372 GB, the total size of the storage is 372 * 3 = 1116 GB.
> ### Must be a multiple of 93 GB.
> glusterfs_cluster_disk_size = 372
> 
> ### Number of nodes in the cluster.
> glusterfs_cluster_nodes = 3
> 
> ### Number of disks on each node.
> glusterfs_cluster_disks_per_node = 1
> 
> 
> 
> ## SLURM CLUSTER STORAGE CONFIGURATION
> ## ---------------------------------------------------------------------------------------------------------------------
> ## Configures how Slurm storages are represented in K8S.
> 
> ### Configuration of the shared storages mounted to Slurm nodes.
> slurm_cluster_storages = {
>     #### "Jail" storage configuration.
>     jail = {
>         ##### Name of the storage. It doesn't matter a lot. Used as a base name for different entities: compute file storage
>         ##### name, mounted device name, K8S PV & PVC names, and the like.
>         name = "jail"
>     
>         ##### Size of the storage in bytes. In case GlusterFS is used, it must not exceed the total size of the storage set
>         ##### in the "glusterfs_cluster_disk_dize" and "glusterfs_cluster_nodes" variables.
>         size = 1115 * (1024 * 1024 * 1024) # 1115Gi
>     
>         ##### Type of the shared storage. Can be either "glusterfs" or "filestore".
>         type = "glusterfs"
>     
>         ##### ID for an existing compute file storage for using it instead of creating a new one. It is relevant, only when
>         ##### type = "filestore".
>         filestore_id = null
>     }
>     
>     #### "Controller spool" storage configuration.
>     controller_spool = {
>         ##### Name of the storage. It doesn't matter a lot. Used as a base name for different entities: compute file storage
>         ##### name, mounted device name, K8S PV & PVC names, and the like.
>         name = "controller-spool"
>     
>         ##### Size of the storage in bytes.
>         size = 100 * (1024 * 1024 * 1024) # 100Gi
>     
>         ##### ID for an existing compute file storage for using it instead of creating a new one.
>         filestore_id = null
>     }
>     
>     #### "Jail submount" storages configuration. If empty, no additional shared storages are mounted to the jail.
>     #### All these storages are initially mounted with 777 permissions and root:root ownerships, but users can change them
>     #### after the Slurm cluster is created.
>     #### It's enough to execute the command like `sudo chmod 755 /datasets && sudo chown bob:bob /datasets` on any of
>     #### the Slurm nodes (login or worker) and these changes will apply to all other nodes in the cluster.
>     jail_submounts = [{
>         ##### Name of the storage. It doesn't matter a lot. Used as a base name for different entities: compute file storage
>         ##### name, mounted device name, K8S PV & PVC names, and the like.
>         name = "datasets"
>     
>         ##### Size of the storage in bytes.
>         size = 100 * (1024 * 1024 * 1024) # 100Gi
>     
>         ##### The absolute path within the jail environment for which data will be available to users.
>         mountPath = "/datasets"
>     
>         ##### ID for an existing compute file storage for using it instead of creating a new one.
>         filestore_id = null
>     }]
> }
> 
> ### Configuration of PVC with the initial jail content. Must be an object with fields name and size (in bytes). If set,
> ### this PVC is used during the initial cluster creation to populate the "jail" shared storage with its content.
> ### See the comment to the "slurm_cluster_create_cr" variable for details.
> slurm_cluster_jail_snapshot = null
> 
> ### Size of the directory storing the slurmd state, that is node-local for each worker.
> slurm_cluster_worker_volume_spool_size = 128 * (1024 * 1024 * 1024) # 128Gi
> 
> 
> 
> 
> 
> ########################################################################################################################
> # SLURM CONFIGURATION
> ########################################################################################################################
> 
> ## SLURM OPERATOR CONFIGURATION
> 
> ### Version of the Slurm operator. Typically, should be left default.
> slurm_operator_version = "0.1.13"
> 
> 
> 
> ## BASIC SLURM CLUSTER CONFIGURATION
> 
> ### Whether to create a Slurm cluster within this terraform. If false, only the operator is created, without a cluster.
> ### This may be useful in scenario when a custom initial content for the "jail" shared storage is needed. It may be
> ### achieved by the following steps:
> ### 1. Apply the terraform with "slurm_cluster_create_cr = false"
> ### 2. Manually create a PVC in the K8S cluster with the content you want to have in the jail environment.
> ### 3. Apply the terraform again with "slurm_cluster_create_cr = true" and "slurm_cluster_jail_snapshot.name" set to
> ###    your PVC name.
> slurm_cluster_create_cr = true
> 
> ### Name of the Slurm cluster.
> slurm_cluster_name = "slurm-dev"
> 
> ### List of SSH public keys that will authorized for user root. After connecting to the cluster as root, the Slurm admin
> ### can create other Linux users with different authorized SSH keys.
> slurm_cluster_ssh_root_public_keys = [
>     "TODO: Put your public SSH key here",
> ]
> 
> 
> 
> ## WORKER NODES CONFIGURATION
> ## ---------------------------------------------------------------------------------------------------------------------
> ## Slurm worker nodes are running on K8S nodes with GPU.
> ## Keep in mind, that not all compute instance resources are available for allocation in containers. The typical K8S
> ## overhead on each node is 2 vCPU, 4 GiB of memory and 50 GiB of the local disk. And it's better to leave a bit more.
> 
> ### Number of worker nodes where slurmd daemon runs. Slurm jobs will be executed on these nodes.
> slurm_cluster_node_worker_count = 2
> 
> ### Resources, dedicated to the slurmd daemon on each worker node. If set to null, the container will have no K8S
> ### resource requests & limits.
> ### The slurmd container is the place where all user jobs are executed. It must have most of the resources of GPU K8S
> ### nodes.
> slurm_cluster_node_worker_slurmd_resources = {
>     cpu_cores               = 156
>     memory_bytes            = 1220 * (1024 * 1024 * 1024) # 1220Gi
>     ephemeral_storage_bytes = 720 * (1024 * 1024 * 1024) # 720Gi
> }
> 
> ### Resources, dedicated to the munged daemon on each worker node. If set to null, the container has will have no K8S
> ### resource requests & limits.
> ### The munged container is used for internal authentication within the Slurm cluster.
> slurm_cluster_node_worker_munge_resources = {
>     cpu_cores               = 2
>     memory_bytes            = 4 * (1024 * 1024 * 1024) # 4Gi
>     ephemeral_storage_bytes = 8 * (1024 * 1024 * 1024) # 8Gi
> }
> 
> 
> 
> ## CONTROLLER NODES CONFIGURATION
> ## ---------------------------------------------------------------------------------------------------------------------
> ## Slurm controller nodes are running on K8S nodes without GPU.
> ## Keep in mind, that not all compute instance resources are available for allocation in containers. The typical K8S
> ## overhead on each node is 2 vCPU, 4 GiB of memory and 50 GiB of the local disk. And it's better to leave a bit more.
> ## Controller & login nodes together should also not allocate all node resources to the fullest, because some space
> ## is needed for the system workload (such as GPU benchmark starter jobs).
> 
> ### Number of controller nodes where the slurmctld daemon runs. The first available controller is primary, and others
> ### are backup ones. When the current primary controller goes down, the first available backup controller takes control.
> ### There is little sense in setting it to more than 3.
> slurm_cluster_node_controller_count = 2
> 
> ### Resources, dedicated to the slurmctld daemon on each controller node. If set to null, the container will have no K8S
> ### resource requests & limits.
> ### The slurmdctld container is the place where the Slurm cluster is controlled from. It must have enough resources for
> ### operation, but there's no sense in giving it more than 16 CPU and 64 GiB of memory.
> slurm_cluster_node_controller_slurmctld_resources = {
>     cpu_cores               = 8
>     memory_bytes            = 32 * (1024 * 1024 * 1024) # 32Gi
>     ephemeral_storage_bytes = 16 * (1024 * 1024 * 1024) # 16Gi
> }
> 
> ### Resources, dedicated to the munged daemon on each controller node. If set to null, the container will have no K8S
> ### resource requests & limits.
> ### The munged container is used for internal authentication within the Slurm cluster.
> slurm_cluster_node_controller_munge_resources = {
>     cpu_cores               = 1
>     memory_bytes            = 2 * (1024 * 1024 * 1024) # 2Gi
>     ephemeral_storage_bytes = 4 * (1024 * 1024 * 1024) # 4Gi
> }
> 
> 
> 
> ## LOGIN NODES CONFIGURATION
> ## ---------------------------------------------------------------------------------------------------------------------
> ## Slurm login nodes are running on K8S nodes without GPU.
> ## Keep in mind, that not all compute instance resources are available for allocation in login nodes. The typical K8S
> ## overhead on each node is 2 vCPU, 4 GiB of memory and 50 GiB of the local disk. And it's better to leave a bit more.
> ## Controller & login nodes together should also not allocate all node resources to the fullest, because some space
> ## is needed for the system workload (such as GPU benchmark starter jobs).
> 
> ### Number of login nodes where the sshd daemon runs. When a user connects to the Slurm cluster by SSH, they are
> ### directed to a random node. Setting this value to more than 1 makes sense only for high availability or for
> ### distributing user sessions across several computationally weak nodes.
> slurm_cluster_node_login_count = 2
> 
> ### Resources, dedicated to the sshd daemon on each login node. If set to null, the container will have no K8S resource
> ### requests & limits.
> ### The sshd container is the place where the Slurm users are connected to. It must have as many resources, as clients
> ### need, but typically not so many because they are used as a thin client.
> slurm_cluster_node_login_sshd_resources = {
>     cpu_cores               = 16
>     memory_bytes            = 64 * (1024 * 1024 * 1024) # 64Gi
>     ephemeral_storage_bytes = 720 * (1024 * 1024 * 1024) # 720Gi
> }
> 
> ### Resources, dedicated to the munged daemon on each login node. If set to null, the container will have no K8S
> ### resource requests & limits.
> ### The munged container is used for internal authentication within the Slurm cluster.
> slurm_cluster_node_login_munge_resources = {
>     cpu_cores               = 1
>     memory_bytes            = 2 * (1024 * 1024 * 1024) # 2Gi
>     ephemeral_storage_bytes = 4 * (1024 * 1024 * 1024) # 4Gi
> }
> 
> 
> 
> ## PERIODIC GPU BENCHMARK CONFIGURATION
> ## ---------------------------------------------------------------------------------------------------------------------
> ## GPU benchmarks are implemented as "all_reduce_perf" NCCL tests. These checks are launched as usual Slurm jobs, which
> ## are periodically scheduled from a K8S CronJob and stay in the same queue with users' workload.
> 
> ### Cron string representing the schedule for running NCCL benchmarks.
> slurm_cluster_nccl_benchmark_schedule = "0 */3 * * *" # Every 3 hours
> 
> ### Configuration of NCCL benchmarks. Most of the parameters are just passed as arguments to all_reduce_perf NCCL test.
> slurm_cluster_nccl_benchmark_settings = {
>     #### The initial size of the data that is transferred at the beginning of the benchmark.
>     min_bytes = "512Mb"
>     
>     #### The final size of the data that is transferred at the end of the benchmark.
>     max_bytes = "8Gb"
>     
>     #### Step factor affecting the number of data transfers.
>     step_factor = "2"
>     
>     ### Benchmark timeout. If the benchmark exceeds it, the Slurm nodes won't be drained, but the K8S job becomes Failed.
>     timeout = "20:00"
>     
>     ### The target value of the "Avg bus bandwidth" result in "all_reduce_perf" NCCL test. If some Slurm worker node shows
>     ### a result less that this threshold, it is drained.
>     threshold_more_than = "420"
>     
>     ### Whether to use Infiniband (true) or NVLink (false) during this test. Changing this variable may require changing
>     ### the "threshold_more_than" variable as well. The normal limit for Infiniband is 42 and for NVLink is 420.
>     use_infiniband = false
> }
> 
> ### Whether to drain Slurm nodes that showed unsatisfactory results. When drained, the details of the benchmark results
> ### are set to the Slurm node Reason field. Users can see it by executing `scontrol show nodes`
> slurm_cluster_nccl_benchmark_drain_nodes = true
> ```
</details>

<details>
  <summary>Notes for ones creating their clusters in Newbius Cloud Sandbox</summary>

> If you are going to run the MLCommons Stable Diffusion benchmark, you can add the following "jail submount" storage
> to your terraform configuration. It will attach the existing storage "mlperf-sd" from the folder "rodrijjke" to your
> cluster at `/mlperf-sd` directory. The storage contains all data required for running this benchmark, so you don't
> need to wait for the download of its bulky dataset.
> ```terraform
> jail_submounts = [
>   # Your storage specs...
>   {
>     # Existing file storage with all mlperf-sd data downloaded:
>     # https://console.nebius.ai/folders/bjeujmv96k3q44m8fg03/compute/filesystems/dp73m665vf426dnehknh/overview
>     name = "mlperf-sd"
>     size = 1500 * (1024 * 1024 * 1024) # 1500Gi
>     mountPath = "/mlperf-sd"
>     filestore_id = "dp73m665vf426dnehknh"
>   },
> ]
> ```
</details>

#### Step 5. Apply terraform
Issue an IAM token for interacting with the cloud: `source ncp_auth.sh`.

Authenticate in the Nebius Container Registry in order to download Helm charts:
```
ncp container registry configure-docker
```

Then you can start by executing `terraform plan`. It will output all resources it's going to create.

If you are OK with it, execute `terraform apply`. Creating all resources usually takes ~15 min.

When it finishes, connect to the K8S cluster and wait until the `slurm.nebius.ai/SlurmCluster` becomes "Available". 
This usually takes ~10 min.

<details>
  <summary>What resources this terraform creates?</summary>

> - K8S cluster
> - VPC (it's also possible to use an existing one)
> - A static IP address
> - Shared storage where the slurm nodes' root directory will be stored. Either a GlusterFS cluster run on a bunch of compute instances, or a compute file storage.
> - A compute file storage for storing Slurm controller state in it and share between primary and backup controllers.
> - The configured number of additional compute file storages that users want to mount into their environment.
> - Several Helm releases:
>   - NVIDIA GPU operator, that propagates GPU drivers and low-level libraries from K8S nodes into containers.
>   - NVIDIA network operator, that propagates InfiniBand drivers and low-level libraries from K8S nodes into containers.
>   - Slurm operator, that creates Slurm clusters.
>   - Slurm cluster storage, that brings shared storages (GlusterFS and/or compute file storage) into K8S pods.
</details>



## How to check the created Slurm cluster

### Connect to your Slurm cluster
In order to connect to your cluster, extract its static IP address from your terraform state:
```
jq '.resources[] | select(.type == "nebius_vpc_address") | .instances[0].attributes.external_ipv4_address[0].address' terraform.tfstate
```

Connect to the cluster by SSH: `ssh -i ~/.ssh/id_ed25519.pub root@<ip_of_slurm_cluster>`. You'll appear on one of the 
login nodes.

Take a look on the list of Slurm workers: `sinfo -Nl`. Make sure they all are in `idle` state.

In order to connect to a specific worker, use the following command: `srun -w <worker-name> -Z --pty bash`. The `-Z`
option states not to allocate any resources and the `--pty` option to launch bash with terminal mode.

Now you can check how it executes compute jobs. Release tarballs offer two kind of checks: [quick](#quickly-check-the-slurm-cluster) and [long](#run-mlcommons-stable-diffusion-benchmark).

Additionally, you can [try out the special features](#try-out-special-features) this Slurm operator provides.

### Quickly check the Slurm cluster

In your terraform release, there is a directory `test`. Enter it: `cd test`.

Run the script that uploads several batch job scripts to your cluster:
```shell
./prepare_for_quickcheck.sh -u root -k ~/.ssh/id_ed25519 -a <ip_of_slurm_cluster>
```

Within an SSH session to the Slurm cluster, execute:
```shell
cd /quickcheck

sbatch hello.sh
tail -f outputs/hello.out

sbatch nccl.sh
tail -f outputs/nccl.out

sbatch enroot.sh
tail -f outputs/enroot.out
```

<details>
  <summary>What do these checks do?</summary>

> - **hello.sh**: performs basic checks of the Slurm cluster: jobs can be executed and resources can be allocated.
> - **nccl.sh**: executes NCCL test "all_reduce_perf" twice: using NVLink and Infiniband.
> - **enroot.sh**: launches jobs inside enroot containers (using pyxis plugin).
</details>

### Run MLCommons Stable Diffusion benchmark

In your terraform release, there is a directory `test`. Enter it: `cd test`.

Run the script that uploads several scripts to your cluster:
```shell
./prepare_for_mlperf_sd.sh -u root -k ~/.ssh/id_ed25519 -a <ip_of_slurm_cluster>
```

Within an SSH session to the Slurm cluster, execute:
```shell
cd /opt/mlperf-sd
./prepare_env.sh
```

This script clones the MLCommons git repository, configures it for our cluster setup and schedules a Slurm job for
downloading datasets & checkpoints.

The actual working directory for this benchmark is located at the root level: `/mlperf-sd`.

Wait until the job finishes. You can track the progress by running `squeue`, or checking the `aws_download.log` output.

Start the benchmark:
```shell
cd /mlperf-sd/training/stable_diffusion
./scripts/slurm/sbatch.sh
```

You can see the benchmark output in the log file created in `./nogit/logs` directory.

If your setup consists of 2 worker nodes with 8 H100 GPU on each, you can compare it with the reference log file:
`./nogit/logs/reference_02x08x08_1720163290.out`

Also, you can execute `./parselog -f nogit/logs/your_log_file` in order to parse your log file and calculate the result.

<details>
  <summary>Usage example</summary>

>```
>$ parselog -f nogit/logs/reference_02x08x08_1720163290.out -g 2xH100
>
> interval |     steps     |     duration     
>----------+---------------+-------------------
>        1 |      0-100    |  23.62s (23618ms)
>        2 |    100-200    |  20.54s (20536ms)
>        3 |    200-300    |  20.54s (20538ms)
>        4 |    300-400    |  20.06s (20059ms)
>        5 |    400-500    |  20.28s (20282ms)
>        6 |    500-600    |  19.97s (19966ms)
>        7 |    600-700    |  20.01s (20012ms)
>        8 |    700-800    |  20.00s (19999ms)
>        9 |    800-900    |  20.17s (20166ms)
>       10 |    900-1000   |  19.95s (19945ms)
>----------+---------------+-------------------
> AVG: 20.51s <= 21.38s (target for 2xH100 GPU)
> min: 19.95s
> max: 23.62s
>```
</details>

### Try out special features

#### Shared root filesystem
You can create a new user on a login node and have it appear on all nodes in the cluster. There's a wrapper script 
`createuser` that:
- creates a new user & group
- adds they to sudoers
- creates a home directory with the specified public SSH key

<details>
  <summary>Usage example</summary>

>```
> $ createuser pierre
>
> Adding user `pierre' ...
> Adding new group `pierre' (1004) ...
> Adding new user `pierre' (1004) with group `pierre' ...
> Creating home directory `/home/pierre' ...
> Copying files from `/etc/skel' ...
> New password: ********
> Retype new password: ********
> passwd: password updated successfully
> Changing the user information for pierre
> Enter the new value, or press ENTER for the default
> 	Full Name []: Pierre Dunn
> 	Room Number []: 123
> 	Work Phone []:
> 	Home Phone []:
> 	Other []: Slurm expert
> Is the information correct? [Y/n] y
> Enter the SSH public key, or press ENTER to avoid creating a key:
> ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIKzxkjzPQ4EyZSjan4MLGFSA18idpZicoKW7HC4YmwgN pierre.dunn@gmail.com
>```
</details>

You can also check how new packages are installed into the shared filesystem:
```
# Install the package on the login node
apt update && apt install -y neofetch

# Run it on a worker node
srun neofetch
```

#### Periodic GPU health checks
The NCCL tests are launched from the `<cluster-name>-nccl-benchmark` K8S CronJob.

You can trigger this job manually if you don't want to wait until the next execution time.

If everything is OK with GPUs on your nodes the CronJob launch will finish successfully.

In order to simulate GPU performance issues on one of the nodes, you can launch another NCCL test with half of available
GPUs just before triggering the CronJob:
```
srun -w worker-0 -Z --gpus=4 bash -c "/usr/bin/all_reduce_perf -b 512M -e 16G -f 2 -g 4"
```
(We set the `-Z` option here, so it will ignore GPUs allocated in concurrent jobs):

After that, `worker-0` should become drained: `sinfo -Nl`. 

You can see the verbose details in the Reason field of this node description: `scontrol show node worker-0`
