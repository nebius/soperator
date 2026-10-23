# Network topology

Soperator describes the cluster's network to Slurm through `topology.yaml`: which switches and
blocks the worker nodes sit on, and in which order. This document explains what Slurm does with
that file, how the operator builds it from Kubernetes node labels and keeps it current, and how an
administrator configures, paces and inspects the result.

## Why topology matters

Slurm uses a topology for two things:

- Job placement. Slurm lands a job on nodes that share a switch or a block when it can, so the
  nodes of one job have the fastest interconnect between them.
- Node order and rank distribution. Slurm orders its nodes by the cluster-default topology, which is
  the order of `SLURM_JOB_NODELIST` and `scontrol show nodes`, and ranks a job's nodes by the
  topology of its partition, which decides which node gets `SLURM_NODEID=0` and the first
  `SLURM_PROCID`. Collective communication is set up from these numbers.

For example, PyTorch takes `MASTER_ADDR` from the first node of `SLURM_JOB_NODELIST` and `RANK`,
`LOCAL_RANK` and `WORLD_SIZE` from `SLURM_PROCID`, `SLURM_LOCALID` and `SLURM_NTASKS`. Both orders
follow the sequence of leaf switches and blocks in `topology.yaml`, so every topology in the file
has to list them in one shared order; otherwise rank 0 may not run on the master node.

One cluster often needs several topologies: HGX nodes sit on an InfiniBand tree, NVL72 racks form
NVLink blocks on top of their tree, and CPU-only nodes have no fabric worth describing.

## How it works

The node topology controller gathers the tier labels of every Kubernetes node into one ConfigMap
and distributes it to the cluster namespaces. The worker topology controller builds `topology.yaml`
from the NodeSets, the worker pods and that ConfigMap, and publishes it as a ConfigMap and a
JailedConfig owned by the SlurmCluster. sconfigcontroller writes the file into the jail and asks
slurmctld to re-read it when the JailedConfig carries a `Reconfigure` action.

### Node labels

A node's position is read from labels `<prefix>/tier-N` on the Kubernetes node, where `tier-0` is
the switch closest to the node, the leaf switch of an HGX node or the NVLink domain of an NVL72
rack, and higher tiers are spines and cores. The prefix defaults to `topology.nebius.com` and is set
by `controllerManager.manager.env.topologyLabelPrefix` in the `soperator` chart. Slurm reads the
values through its hostlist parser, so a label must not contain spaces, commas, brackets or braces,
and must not end in more than 18 digits. Tiers must be contiguous. A node with missing labels sits
under its fabric's `unknown` switch or block; one with invalid labels does the same and is reported.

### Fabrics

`spec.topology.fabric` on a NodeSet names the root switch all of its nodes hang under, so Slurm
never spans a job across fabrics; NodeSets without one share the root `root`. It is a NodeSet
property rather than a node label because a node has to be placed in the file even when it is
powered down or not scheduled, and then there is no Kubernetes node to read a label from. Such nodes
go under `<fabric>.unknown`, written as a switch only when it has nodes and as a block always.

### Topology types

- `tree` lists every tier from the labels as switches, nodes under their lowest one.
- `block` lists one block per `tier-0` switch, ordered like the tree's leaves, plus the unknown
  block; `blockSizes` tell Slurm how blocks aggregate.
- `flat` lists no nodes: it tells Slurm there is nothing to optimize.

Tree and block topologies list GPU NodeSets only. A CPU-only node on a fabric would look one hop
away from the GPUs, and a block partition refuses nodes outside any block, so CPU-only NodeSets are
reached through a flat topology.

## Configuring topologies

### NodeSets

Three NodeSet fields matter: `gpu.enabled`, which admits the NodeSet to tree and block topologies,
`topology.fabric`, and `aggregateSlurmNodeSets`, a name shared by several NodeSets that topologies
and partitions can reference instead of listing each rack. In the `nodesets` chart:

```yaml
nodesets:
  - name: hgx
    replicas: 32
    gpu:
      enabled: true
    topology:
      fabric: "gpucluster-2a9e"
  - name: nvl72
    range:                     # renders nvl72-rack0 and nvl72-rack1, both listing "nvl72"
      firstOrdinal: 0
      lastOrdinal: 1
      suffix: "-rack"
    replicas: 18
    gpu:
      enabled: true
    topology:
      fabric: "gpucluster-1f0c"
  - name: cpu
    replicas: 4
```

### SlurmCluster

`spec.topology.topologies[]` declares the named topologies. Each has a `name`, a `topo.type` of
`tree`, `block` or `flat`, for blocks the `topo.blockSizes` as the base size followed by
power-of-two multiples, an optional `clusterDefault`, and `nodeSetRefs` naming NodeSets, aggregate
NodeSets or `ALL`; empty refs mean every NodeSet. Exactly one topology must be the cluster default.

Validation has two outcomes. Duplicate names, a missing or doubled cluster default, or a block
topology without `blockSizes` stop publishing for the whole cluster until fixed. An unsupported
type or malformed block sizes drop that one topology from the file and keep the others. Both are
recorded as events on the SlurmCluster.

### Partitions

`spec.partitionConfiguration.partitions[].topologyRef` renders `Topology=<name>` on the partition;
without it the partition uses the cluster default. It is a field rather than text in `config`
because the renderer drops a reference to a topology the SlurmCluster does not declare: slurmctld
rejects a partition bound to a topology it cannot see, and NodeSets, the SlurmCluster and the
topology file are applied in any order during bootstrap. A partition should only mix nodes that one
topology can place together, so GPU and CPU-only NodeSets belong in different partitions.

### Example

SlurmCluster configuration:
```yaml
topology:
  topologies:
    - name: ib
      clusterDefault: true
      topo: {type: tree}
      nodeSetRefs: [hgx, nvl72]
    - name: nvl
      topo: {type: block, blockSizes: [18, 36]}
      nodeSetRefs: [nvl72]
    - name: flat
      topo: {type: flat}
      nodeSetRefs: [cpu]
partitionConfiguration:
  configType: structured
  partitions:
    - {name: hgx, nodeSetRefs: [hgx], topologyRef: ib, config: "State=UP"}
    - {name: nvl72, nodeSetRefs: [nvl72], topologyRef: nvl, config: "State=UP"}
    - {name: cpu, nodeSetRefs: [cpu], topologyRef: flat, config: "Default=YES State=UP"}
```

The rendered `topology.yaml`:

```yaml
- topology: ib
  cluster_default: true
  tree:
    switches:
        - switch: gpucluster-1f0c
          children: gpucluster-1f0c.unknown,t2-90be2f
        - switch: t2-90be2f
          children: t1-1c7d4e
        - switch: t1-1c7d4e
          children: t0-a3f0b9,t0-f82c1a
        - switch: t0-a3f0b9
          nodes: nvl72-rack0-[0-17]
        - switch: t0-f82c1a
          nodes: nvl72-rack1-[0-16]
        - switch: gpucluster-1f0c.unknown
          nodes: nvl72-rack1-17
        - switch: gpucluster-2a9e        # the HGX fabric follows
          ...
- topology: nvl
  cluster_default: false
  block:
    block_sizes: [18, 36]
    blocks:
        - block: t0-a3f0b9
          nodes: nvl72-rack0-[0-17]
        - block: t0-f82c1a
          nodes: nvl72-rack1-[0-16]
        - block: gpucluster-1f0c.unknown
          nodes: nvl72-rack1-17
- topology: flat
  flat: true
```

## How the file is built

The operator first builds one forest of the whole cluster: a root per fabric, a vertex per switch
named in a label, and every node attached to its lowest switch or to its fabric's `unknown` vertex.
Under each switch the children are sorted by tier from the highest down, then by name, with
`unknown` last. Fabrics are sorted by name.

Each topology is a pre-order walk of the part of that forest its NodeSets occupy. That is why a
tree and a block topology over the same NodeSets list leaves and blocks in the same order, and why
a topology over fewer NodeSets keeps the cluster-wide order.

Labels that contradict each other, a switch with two parents or at two tiers, stop the build rather
than being voted on. A topology whose refs reach no node is published as a placeholder with a
single empty switch or block, since an empty list makes slurmctld exit.

## How Slurm learns about changes

### Kinds of change

Each pass compares the new file with the published one and sorts the difference by what slurmctld
needs in order to learn it:

| Change | Examples |
| --- | --- |
| Identical | nothing differs |
| Registered | node lists, inner switches, the order inside a `children` list |
| Reordered | leaf switches or blocks reordered, appearing or disappearing; `unknown` leaves excepted |
| Restructured | a topology added, removed, retyped or made default; `blockSizes`; a block appearing |

### Node moves need no reconfigure

A worker tells slurmctld its own switch or block when its slurmd starts, so a pod moving to another
Kubernetes node only republishes the file. Until the next re-read, that node's rank may disagree
with the node order; re-reading for every pod move would restart slurmd everywhere instead.

### Structural changes reconfigure at once

The operator puts a `Reconfigure` action on the JailedConfig, sconfigcontroller runs the reconfigure
and waits for every slurmd to restart, and the request is withdrawn once `ReconfigurePerformed`
names the current generation. The request is raised before the content is published, so a failed
publish cannot lose it, and it is withdrawn before anything else is published, so a stale request
cannot turn a node move into another reconfigure.

### Reordered changes are deferred

`scontrol reconfigure` resets slurmctld's internal timers, and two reconfigures closer together than
the longest timeout in `slurm.conf` keep that timeout from ever expiring. Reorders are the changes
that can wait, so they are paced.

A reorder is published at once, so the jail always holds the current file, while the request waits
for three things: a cooldown since the last reconfigure, then the order staying unchanged for a
debounce, but never longer than a cap after the first held change. Repeated reorders inside one
window collapse into one reconfigure, and an immediate reconfigure settles a held one on the way.

The wait is recorded in annotations on the JailedConfig, so an operator restart loses nothing. A
node powering down or up moves between its switch and `unknown`, which counts as a node move, not a
reorder.

## Operating it

### Flags and Helm values

Set through `controllerManager.manager.args` in the `soperator` chart:

| Flag | Default | Meaning |
| --- | --- | --- |
| `--topology-reconfigure-cooldown` | `15m` | Least time between the previous reconfigure and a deferred one; `0` disables it |
| `--topology-reconfigure-debounce` | `1m` | How long the order has to stay unchanged before a deferred reconfigure runs |
| `--topology-reconfigure-debounce-cap` | `15m` | Most a deferred reconfigure waits for the order to settle; `0` disables the cap |

### Kubernetes events

Events on the SlurmCluster come from the worker topology controller; sconfigcontroller records
`Reconfigured` and `ReconfigureFailed` on the JailedConfig.

| Reason | Type | Meaning |
| --- | --- | --- |
| `InvalidTopologySpec` | Warning | A topology is left out of the file; fix its `topo` |
| `InvalidTopologyConfig` | Warning | Nothing is published; fix names, the cluster default or block sizes |
| `ConflictingTopologyLabels` | Warning | Nothing is published; fix the node labels named in the event |
| `InvalidNodeTopologyLabels` | Warning | The listed nodes sit under `unknown`; fix their labels |
| `TopologyReachesNoNode` | Warning | A placeholder is published; check `nodeSetRefs` or wait for the NodeSets |
| `ReconfigureRequested` | Normal | A reconfigure was asked for, with the cause |
| `ReconfigureDeferred` | Normal | A reorder is held, with the wait |

## Dynamic node registration

The topology file is not the only way slurmctld learns where a node is. When a worker pod starts,
its slurmd registers with `--conf "topology=<name>:<fabric>:...:<leaf>"` for every topology that
lists it, built from the same node labels. slurmctld then places the node under that switch or
block at once, adding the switch to its tables if the file did not have it yet.

Without this, a new or rescheduled worker could only be placed after a `scontrol reconfigure` that
re-read a file naming its switch or block, and its startup would wait on that. Reconfigures are
kept rare on purpose: each one resets every timer inside slurmctld, so timeouts such as the slurmd
and resume timeouts never expire when reconfigures follow each other too closely, and each one
restarts slurmd on every node. Registration is what lets node moves stay out of that path, and
the static file is there for the order of switches and blocks and for slurmctld restarts.
