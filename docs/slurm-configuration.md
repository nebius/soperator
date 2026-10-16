# Slurm configuration

Slurm configuration in a Soperator cluster is declared in the `SlurmCluster` resource.
The operator renders it into Kubernetes ConfigMaps, and a dedicated controller writes the files into the shared root
filesystem (the jail) that every node mounts as `/`. This is the only path that lasts.
Changes made with `scontrol`, or by editing files inside the jail, are reverted by the next reconciliation.

## Where the configuration comes from

The `SlurmCluster` spec has several fields that end up in `slurm.conf` and the files next to it. The
full field documentation lives in [api/v1/slurmcluster_types.go](../api/v1/slurmcluster_types.go).

- `slurmConfig`: a typed subset of `slurm.conf` options (`MaxJobCount`, `MinJobAge`, `Prolog`,
  `Epilog`, `DefMemPerNode` and so on). Each set field is emitted as `Key=value`.
- `customSlurmConfig`: raw `slurm.conf` text. It is included after the generated configuration, so a
  value set here overrides the generated one. Soperator does not validate it.
- `partitionConfiguration`: partitions, chosen by `configType`:
  - `default` renders two partitions, `main` (the default one) and `hidden` (used by health checks);
  - `custom` copies the `rawConfig` lines that start with `PartitionName`;
  - `structured` renders `partitions`, each with `nodeSetRefs` or `isAll`, an optional `topologyRef`
    and a free-form `config` string.
- `customCgroupConfig` and `plugStackConfig`: `cgroup.conf` and SPANK plugins in `plugstack.conf`.

Worker nodes are described by separate `NodeSet` resources (see
[api/v1alpha1/nodeset_types.go](../api/v1alpha1/nodeset_types.go)). Their `replicas`, `nodeConfig`
(features, GRES, static node options) and `slurmd.port` become the `NodeName` and `NodeSet` lines of
`slurm.conf` and the per-node entries of `gres.conf`. A change to a `NodeSet` re-renders the
configuration the same way a change to the `SlurmCluster` does.

The operator renders all of this into the ConfigMap `<cluster>-slurm-configs` (see
[internal/render/common/configmap.go](../internal/render/common/configmap.go)).
`slurm.conf` itself is only an entrypoint: it includes `slurm_base.conf.noedit` with the generated configuration and
`slurm_k8s_extra.conf.noedit` with the contents of `customSlurmConfig`.
The ConfigMap also holds `slurm_rest.conf`, `cgroup.conf`, `plugstack.conf`, `gres.conf` and `mpi.conf`.
Every file starts with a header saying that it is managed by Soperator and that edits are overwritten.

Topology is rendered separately into `/etc/slurm/topology.yaml` through the
`<cluster>-topology-config` ConfigMap, see [`topology.md`](topology.md).

Kubernetes rejects a ConfigMap whose total size exceeds 1 MiB (`Too long: may not be more than
1048576 bytes`), and all Slurm configuration files share one ConfigMap. This is why Soperator describes
nodes with ranges: one `NodeName=worker-[0-N]` line per NodeSet and one `NodeSet=` line next to it.
A typical cluster uses a few kilobytes. Keep anything you add through `customSlurmConfig` or custom
partitions compact as well: a line per node, for example per-node `Features` or partitions that
enumerate nodes one by one, grows with the cluster and can make the ConfigMap impossible to update.
Use host ranges and NodeSet references instead.

## How it reaches the nodes

Together with the ConfigMap, the operator creates a `JailedConfig` resource with the same name.
It maps every key of the ConfigMap to a path under `/etc/slurm` and asks for `Reconfigure` as the update action.

The `<cluster>-sconfigcontroller` Deployment watches the `JailedConfig` and its ConfigMap.
On every reconcile it writes all files of the group into the jail, replacing each one atomically.
When the content changed, it then calls the slurmrestd reconfigure endpoint and waits until `slurmd` on every
responding node has restarted. There is no periodic resync: a reconcile runs when the ConfigMap or the
`JailedConfig` changes, when the sconfigcontroller pod restarts, or when its leader changes.

Inside the controller, worker and login containers `/etc/slurm` is a symlink to the jail copy, so the
daemons and the users see the same files. `kubectl get jailedconfig` shows whether the files were
written and whether the reconfigure finished, and the `Reconfigured` and `ReconfigureFailed` events are
recorded on the object.

## How to change the configuration

Change the `SlurmCluster` resource and let the operator do the rest.

- Partitions: edit `partitionConfiguration`. With the structured type:

  ```yaml
  partitionConfiguration:
    configType: structured
    partitions:
      - name: main
        isAll: true
        config: "Default=YES PriorityTier=10 PreemptMode=OFF MaxTime=INFINITE State=UP"
      - name: low
        nodeSetRefs: ["worker"]
        config: "Default=NO PriorityTier=1 PreemptMode=REQUEUE GraceTime=120 State=UP"
  ```

- Any other `slurm.conf` option: put it into `customSlurmConfig`. For example, one `Licenses=` line
  adds your own licenses; it is included after the generated configuration and Slurm keeps only the
  last `Licenses=` line, so it must still list the ones Soperator defines (see [`active-checks.md`](active-checks.md)).
- `cgroup.conf` and SPANK plugins: `customCgroupConfig` and `plugStackConfig`.

If the `SlurmCluster` resource is itself produced by Helm, Flux or Terraform, change the values there.
Editing the resource directly works until the next sync, which restores it from the values.

## What is not persistent

Slurm offers several ways to change its configuration at runtime, and Soperator adds a few layers on top.
From the least to the most durable:

1. `scontrol create`, `scontrol update` and `scontrol delete` applied to partitions, and
   `scontrol update NodeName=... ` applied to `Features`, `Gres` or `Weight`. These changes live only
   in slurmctld memory. They are lost on `scontrol reconfigure`, which the sconfigcontroller triggers
   on any configuration change, and on a slurmctld restart. Node state (`drain`, `resume`),
   reservations and jobs are different: Slurm keeps them in `StateSaveLocation` and they survive
   restarts.
2. Editing files under `/etc/slurm` inside the jail. The edit survives daemon restarts, but the next
   sconfigcontroller reconcile rewrites the whole group of files. That reconcile can be triggered by
   something unrelated to your edit, for example a replaced worker node that changes the topology.
3. Editing the `<cluster>-slurm-configs` ConfigMap. The next `SlurmCluster` reconcile renders it again
   from the spec.
4. Editing the `SlurmCluster` resource when it is managed by Helm, Flux or Terraform. The next sync
   restores it from the values.

The first case is the easiest to get wrong, because the change looks applied until the next
reconfigure. A partition created with `scontrol` disappears from `slurm.conf` at that moment, and
jobs submitted to it stay in the queue without a partition to run in until it is added to the
`SlurmCluster` spec.

`scontrol show config` and `scontrol show partition` only show what slurmctld holds in memory right
now and say nothing about where it came from. To confirm that a change is permanent, look at the
`SlurmCluster` spec or at the values that produce it.

To make this visible, `scontrol` inside the jail is a thin wrapper around the real binary
(`/usr/bin/scontrol.real`). When the arguments describe one of the changes from the first case, it
prints a warning to stderr and then runs the command unchanged: stdout and the exit code are those of
the real `scontrol`. The warning is printed for scripts and AI agents as well, since they run such
commands on behalf of users. Automation that parses stderr of these commands can set
`SOPERATOR_SCONTROL_QUIET=1` in its environment to suppress it.
