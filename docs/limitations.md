# Limitations
Here is a list of some key limitations we're aware of:


### GPUs are required
You can't create a Slurm cluster with nodes that don't have GPUs.

However, supporting CPU-only clusters seems pretty straightforward and we're already working on it.


### No terraform recipes for clouds other than Nebius
We currently only provide a terraform recipe for Nebius cloud.


### Scaling clusters down
While you can easily change the number of worker nodes by tweaking a value in the YAML manifest, it only works smoothly
when scaling up. If you try to scale the cluster down, you’ll find that the deleted nodes remain in the Slurm
controller’s memory (and show up in `sinfo` output, for example). You can manually remove them (using `scontrol`) if
they bug you.

It doesn't interfere users to launch their jobs and will be fixed soon.


### Single-partition clusters
The Slurm's ability to split clusters into several partitions (= job queues) isn't supported now.

We'll implement it if there is a demand. The idea is that nodes in different partitions will be able to vary (e.g.
equipped with different GPU models, use different container images, have different storages mounted, etc.)


### Software versions
Our list of supported software versions is pretty short right now:
- Linux distribution: Ubuntu [22.04](https://releases.ubuntu.com/jammy/).
- Slurm: versions `25.11.2`.
- CUDA: version [12.4.1](https://developer.nvidia.com/cuda-12-4-1-download-archive).
- Kubernetes: >= [1.29](https://kubernetes.io/blog/2023/12/13/kubernetes-v1-29-release/).
- Versions of some preinstalled software packages can't be changed.

Other versions may also be supported, but we haven't checked it yet. It would be cool if someone from the community
tried to launch Soperator on a different setup and leave a feedback.


### Some software can't be upgraded when the cluster is running
Although users can install or modify software in the shared environment, it doesn't apply to some low-level packages
directly bound to GPUs (CUDA, NVIDIA drivers, NVIDIA container toolkit, enroot, etc.).

Such software versions must be explicitly supported in container images Soperator uses.


### Slurm configuration is limited by what the operator can do
While setting some configuration options for Slurm should indeed be done by Soperator, there are such ones that some
people would like to customize themselves. Not all of this is supported.


### Some kernel parameters aren't configurable
For example, Soperator sets some [sysctl](https://man7.org/linux/man-pages/man8/sysctl.8.html) params on its own, and
it's not configurable by the user.

Try this solution as it is and if something doesn't work, let us know, we will fix it.


### Slurm integration with LDAP isn't supported
You can only use Linux users & groups for now.
