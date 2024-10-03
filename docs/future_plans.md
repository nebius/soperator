# Future Plans

> [!IMPORTANT]
> If you are interested in any of these features (or any other), feel free to report this on GitHub issues. We implement
> features according to user needs, but we may not know about something if no one tells us about it.


### Slurm accounting
Soperator doesn't manage `slurmdbd` daemons and databases for storing accounting data (dividing users into accounts,
departments and organizations, QoS & priorities configuration, storing job history & batch scripts, etc.)

We're already working on brining accounting to this solution. Various databases will be supported, and Soperator will
use MariaDB by default.


### Better security & isolation
Users actions are isolated from core components such as Slurm daemons because they use a separate rootfs that clearly
defines the boundary between users' responsibility and administrator's / Soperator's one. It also makes the solution
more durable preventing users from accidentally damaging Slurm.

However, users still can damage the cluster because not all global Linux resources are isolated. We can do this further
by creating more types of Linux namespaces in our SPANK plugin (i.e. process namespace for isolating process trees and,
signal scopes, user namespace for taking away superuser rights on host environments, IPC namespace, etc.). Implementing
this is in our todo list.

Another consideration is that most of the containers Soperator creates now are "privileged". It would be much better if
we grant only required Linux capabilities & set up more detailed policies for AppArmor / SELinux.


### On-demand nodes
Soperator already makes it much easier to scale your Slurm cluster, but we're not stopping on this. We want to
automatically bootstrap new compute nodes (within configured limits) when there are queued jobs that need them.

This will allow users to use only those resources that they need at a time.


### Network topology-aware job scheduling
Thanks to Slurm’s topology feature, we can support detailed configuration of the network topology to guide Slurm how
to schedule jobs with the maximum efficiency.

We're going to implement automatic network observation and make Slurm schedule jobs on nodes with the shortest network
path. This will allow users to complete computations faster (especially for model training with its huge amount of
data exchange between nodes).


### Automatic replacement of underperforming K8s nodes
Right now, our solution only drains Slurm nodes that fail health checks, leaving it up to the user to deal with them.
We’re planning to implement fully automatic replacement of such nodes that will be transparent to Slurm users.


### Jail backups
While the shared-root feature makes your life easier, it also increases the risk of breaking the filesystem for the
entire cluster. So we’re going to backup jails periodically to improve the durability of our Slurm clusters.


### Automatic external checkpointing
There is a promising (though still experimental) NVIDIA project
[cuda-checkpoint](https://github.com/nvidia/cuda-checkpoint).
It allows users to take external checkpoints of Linux processes that use
GPUs, saving them to the filesystem, so they can resume these processes later.

Integrating cuda-checkpoint with Slurm could free users from writing complex application-level code for checkpointing.

We're going to give this project a try.


### More hardware health checks
Running NCCL tests for benchmarking GPUs is good but not sufficient. We're going to implement other checks as
well. This includes checking the filesystem & network, as well as other kinds of GPU checks.


### Multi-jail clusters
We think it might be useful for some users to have clusters with multiple "jail" environments.

If we implement this, when starting a job, you will be able to choose in which jail to execute it by setting a special
command argument.


### Setups with node-local jails or without jail
If some users will find the shared-root feature too expensive due to loss of the file system performance, we will
support node-local jails. Nothing should change except for the fact that you'll need to prepare filesystems of new
nodes on your own.


### Slurm REST API & GUI
It's a shame that there are no solutions at the moment that provide some handsome graphical user interface where users
can submit and view their jobs, because not all ML engineers are well-versed in Linux. There are some solutions but they
use outdated approaches with parsing command outputs.

If it will be of interest to users, we'll develop a new GUI solution that uses Slurm REST API.


### Better metrics exporter
At the moment, we use [prometheus-slurm-exporter](https://github.com/vpenso/prometheus-slurm-exporter) for gathering
Slurm metrics. Unfortunately this project is no longer maintained, so we're considering developing a new solution that
will use Slurm REST API as well.
