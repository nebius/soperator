# Network topology

Soperator describes the cluster's network to Slurm so the scheduler can place jobs close together on
the fabric. Since Slurm 25.05 a cluster is described as one or more named topologies that partitions bind to
individually.

- [When you need several topologies](#when-you-need-several-topologies)
- [What gets rendered](#what-gets-rendered)
- [Quick start](#quick-start)
- [Configuration reference](#configuration-reference)
- [How topologies are built](#how-topologies-are-built)
- [Verifying the result](#verifying-the-result)
- [Troubleshooting](#troubleshooting)

## When you need several topologies

One topology is enough when every node sits on the same fabric and the same plugin suits all of
them. Reach for several when that stops being true:

- InfiniBand GPU NodeSets next to Ethernet-only CPU NodeSets — the GPU side benefits from
  `topology/block`, the CPU side has no meaningful hierarchy to optimize;
- several independent fabrics that no job should span;
- the same nodes need two views at once, for example blocks for tightly-coupled training jobs and a
  switch tree for everything else.

Before this existed, all of those had to be squeezed into one flat topology, which degrades
scheduling decisions, or worked around through `customSlurmConfig`.

## What gets rendered

Soperator renders the cluster's named topologies into `/etc/slurm/topology.yaml`, written to the
`<cluster>-topology-config` ConfigMap by the topology controller and mounted into the jail through a
`JailedConfig`.

A cluster that declares no `spec.topology.topologies` gets no topology config at all: there is
nothing to render, and the controller leaves it alone. Every topology a cluster needs is declared
in `spec.topology.topologies`, normally at provisioning time.

Do not remove the final named topology from a running cluster. The controller does not uninstall an
already published topology config when the list becomes empty, so disabling topology requires a
separate migration that removes the topology `JailedConfig` and its files.

## Quick start

The example below describes a cluster with two GPU NodeSets on InfiniBand and one CPU NodeSet on
Ethernet, and gives the GPU partition a block topology of its own.

### 1. NodeSets: declare the fabric

Only needed when nodes really sit on separate fabrics. In the `nodesets` chart:

```yaml
nodesets:
  - name: h100
    topology:
      fabric: "ib-a"
  - name: h200
    topology:
      fabric: "ib-a"
  - name: cpu
    # No fabric: stays on the default "root".
```

Workers of a NodeSet are grouped under a root switch named after the fabric, so Slurm never
schedules a job across fabrics.

### 2. SlurmCluster: define the topologies and bind the partitions

In the `slurm-cluster` chart:

```yaml
topology:
  topologies:
    - name: ib-gpu
      topo:
        type: block
        blockSizes:
          - 4
          - 16
      nodeSetRefs:
        - h100
        - h200
    - name: eth-cpu
      clusterDefault: true
      topo:
        type: tree
      nodeSetRefs:
        - cpu

partitionConfiguration:
  configType: structured
  partitions:
    - name: gpu
      nodeSetRefs:
        - h100
        - h200
      topologyRef: ib-gpu
      config: "Default=NO State=UP"
    - name: main
      isAll: true
      config: "Default=YES State=UP"
```

### 3. What comes out

`/etc/slurm/topology.yaml`:

```yaml
- topology: ib-gpu
  cluster_default: false
  block:
    block_sizes:
        - 4
        - 16
    blocks:
        - block: nvl0
          nodes: h100-[0-7]
        - block: nvl1
          nodes: h200-[0-7]
- topology: eth-cpu
  cluster_default: true
  tree:
    switches:
        - switch: root
          children: unknown
        - switch: unknown
          nodes: cpu-[0-31]
```

and in `slurm.conf`, where partitions reference NodeSets by the `NodeSet=` lines Soperator also
emits:

```
NodeSet=h100 Nodes=h100-[0-7]
NodeSet=h200 Nodes=h200-[0-7]
PartitionName=gpu Nodes=h100,h200 Topology=ib-gpu Default=NO State=UP
PartitionName=main Nodes=ALL Default=YES State=UP
```

`main` sets no `topologyRef`, so it uses the topology marked `clusterDefault`.

Slurm ranks nodes by the position of their leaf in `topology.yaml`: the leaf switch of a tree, or
the block of a block topology. Two orderings follow from it. The node table, which is what
`SLURM_JOB_NODELIST` and `scontrol show nodes` print, follows the cluster-default topology when
`TopologyParam` carries `SwitchAsNodeRank` or `BlockAsNodeRank`. The task ranks of a step, behind
`SLURM_NODEID`, follow the partition's topology. Slurm does not reconcile the two, so Soperator
renders every topology from one shared leaf order: a tree's leaf switches and a block topology's
blocks are both listed in it, and a topology covering a subset of the NodeSets lists its leaves in
the same relative order. The two orderings then agree whichever plugin the cluster default and the
partition use.

Leaves are grouped by `tier-0`, so a leaf switch and a block carry the same name. Soperator orders
them by fabric, then by their InfiniBand paths, comparing the highest numbered tier first and
continuing down to `tier-1`. Each leaf gets its own path, including when one NodeSet spans several
racks. In a tree, each switch is written right before the first of its leaves, so a consistently
labelled fabric reads as a pre-order walk: the fabric root, its first spine, that spine's leaves,
the next spine, and so on. Node lists inside leaves keep their existing rendering.

Paths come from Kubernetes node labels, independently of which worker pods are currently scheduled.
Nodes without usable labels are ignored; paths without `tier-1` still use their known higher tiers.
Missing ancestors are filled from unambiguous switch relationships on other nodes. Tier numbers
remain part of the comparison, so names from different tiers are never compared, and a missing tier
sorts after a known one: a leaf whose lower tiers are unknown lands after the fully labelled leaves
under the same switch. Equal paths and leaves without known paths use name order; leaves without
known paths and then the catch-all `unknown` leaf come last within their fabric.

A Kubernetes node without a `tier-0` label is the one case the plugins cannot agree on: a tree
makes its lowest labelled tier a leaf switch, while a block topology files the node under `unknown`.

If nodes within a block disagree, Soperator uses the path most of the block's nodes agree on, then
the most complete one, then the one of the first Kubernetes node by name. Conflicts are reported as
one aggregated error log record and one `BlockIBTopologyConflict` warning event per reconcile, with
the number of blocks and up to five examples. Only new conflicts, or conflicts whose selected path
changed, are reported; a conflict is reported again after it was resolved and returns, and after an
operator restart.

When `topo.blockSizes` is not set, Slurm takes the base block size from the node count of the first
block that has nodes in the file. With IB ordering that block is the first populated one in path
order, so its size may change with the scheduled workers. Set `blockSizes` explicitly for block
topologies.

Changes to the ordered list of block names request a Slurm reconfigure after the updated file is
published. This includes blocks being added or removed, for example when workers are scheduled into
a new rack or a rack is powered down. Slurm rejects a node registering into a block it has not
loaded, so a new block needs a re-read. A change that only reorders or removes blocks is deferred,
see [Deferred reconfigures for reordered leaves and blocks](#deferred-reconfigures-for-reordered-leaves-and-blocks).
Changes to node lists inside the same blocks do not request a reconfigure. The catch-all `unknown` block is rendered for every fabric in scope, without nodes
when all of them are placed, so a single rescheduled worker does not add or remove a block.

Upgrading the operator to a version with block ordering usually changes the block order in the
file, so each cluster with a block topology gets one reconfigure after the upgrade. A rollback does
the same.

`features/topology_block.feature` checks the block order with two scenarios.

The sorting scenario checks Soperator's ordering against the `topology.nebius.com/tier-N` labels
read from the Kubernetes nodes, after Slurm has loaded the published order. At every tier, blocks
whose nodes agree on the switches from that tier up must be consecutive. Blocks whose nodes
disagree there, such as racks with rail-optimized leaves, and blocks with gaps in their labels are
left out of that tier's check. Blocks without IB labels must follow the labelled ones, and unknown
blocks must come last. It skips when no block topology has blocks under two different switches.

The rank scenario checks rank distribution by running one task per node across multiple blocks and
comparing `SLURM_PROCID` against the rendered block order. Slurm builds per-job node ranks from the
`block_index` of the partition's topology, and `--distribution=block` assigns task ranks by them.
`SLURM_NODEID` is the node's position in the job's node list, which follows slurmctld's node table.
That table is ordered by block only with `TopologyParam=BlockAsNodeRank` and only for the
cluster-default topology, so it need not follow block order. The scenario records both values for
diagnostics. It tries the available block partitions, including those using the cluster-default
topology. It selects two blocks in the same allocation group and requests `baseBlockSize + 1` idle
nodes, capped at 64 nodes; a smaller job cannot span two base blocks. The base block size is the
`BlockSize` slurmctld reports, since without `blockSizes` it was fixed at the last reconfigure. It
skips when no such pair has enough idle workers. Before selecting workers, it waits for Slurm to
report the currently published block order, refreshing the ConfigMap while waiting, and then reads
the idle workers again. Node order within each block is unconstrained.

## Configuration reference

### `spec.topology.topologies[]`

| Field | Required | Meaning |
| --- | --- | --- |
| `name` | yes | Identifies the topology. Partitions reference it, and workers join it at slurmd registration with `--conf "topology=<name>:<unit>"`. |
| `topo.type` | yes | `tree`, `block` or `flat`. |
| `topo.blockSizes` | no | Planning base block size followed by any higher-level sizes to enforce. Each successive value must be a power of two larger than the previous one. Only for `type: block`. |
| `clusterDefault` | no | Marks the topology used by partitions without a `topologyRef` and by cluster-wide operations not tied to a partition. |
| `nodeSetRefs` | no | NodeSets the topology covers. `ALL`, like an empty list, covers every NodeSet. A `tree` or `block` topology still lists only the GPU NodeSets among them. |

`clusterDefault` resolution follows Slurm's own rule and never fails the render:

- several topologies set it — the first one wins, the rest are cleared with a log line;
- none sets it — the first entry is promoted, with a log line.

There is no way to end up without a cluster default, and that is deliberate. Slurm sorts the
topologies so that flagged ones come first and then uses index 0 unconditionally — both for
partitions with no `Topology=` and for cluster-wide operations such as slurmctld-to-slurmd message
forwarding. Leaving nothing flagged would not remove the default, it would just leave which topology
serves those operations up to the order of the file, so the operator writes the choice out
explicitly instead.

Topologies may overlap. Slurm lets a node belong to several topologies at once, so describing the
same NodeSet as a tree in one topology and as blocks in another is valid, and the worker registers
itself into both:

```yaml
topologies:
  - name: as-blocks
    topo:
      type: block
    nodeSetRefs: [h100]
  - name: as-tree
    clusterDefault: true
    topo:
      type: tree
    nodeSetRefs: [h100]
```

A `nodeSetRefs` entry that matches no NodeSet is dropped, but the topology itself is still written
out, carrying a single empty switch or block. Emitting it keeps a partition bound to it resolving,
and the shape is the smallest Slurm accepts: an empty switch or block list is fatal. This state is
routine while a SlurmCluster and its NodeSets are applied one after another.

### `spec.partitionConfiguration.partitions[].topologyRef`

Available for `configType: structured`. Renders `Topology=<name>` on the partition line, mapping to
the `Topology` partition option of `slurm.conf`: *"Name of the topology, defined in topology.yaml,
used by jobs in this partition."* When empty, the partition uses the topology marked
`clusterDefault`.

A ref naming no declared topology is dropped, with a warning comment in `slurm.conf` rather than a
failed render. Being declared is enough: every declared topology reaches the file, empty if its
NodeSets have not arrived yet.

```
#WARNING: Partition typo references undefined topology nope, ignoring it
```

### What each type lists

| Type | Lists |
| --- | --- |
| `flat` | no nodes at all; applies no placement optimization |
| `tree` | the nodes of the GPU NodeSets it covers, as a switch hierarchy |
| `block` | the nodes of the GPU NodeSets it covers, grouped into blocks |

CPU-only NodeSets are never listed by a `tree` or a `block`, `nodeSetRefs: ALL` included. They sit
on no fabric, and placing them under a switch would tell the scheduler they are one hop from the GPU
nodes. Their workers are told to join only the topologies that list them, so a CPU-only worker never
registers into a fabric topology.

A topology that ends up listing nothing — a `tree` reaching only CPU-only NodeSets, for instance —
is left out of the file, and any partition bound to it loses its `Topology=` binding with a warning.

The operator generates no topology of its own. Every topology a cluster needs is declared in
`spec.topology.topologies`, normally at provisioning time.

### Clusters with no fabric to optimize on

Nodes without an InfiniBand fabric — CPU-only NodeSets, ephemeral workers — have no meaningful
switch hierarchy. Describing them with a tree or a block invents one. Give them a `flat` topology
instead, which states plainly that there is nothing to optimize and lists no nodes at all.

A cluster that is entirely CPU-only needs one entry and nothing else:

```yaml
topology:
  topologies:
    - name: cpu
      clusterDefault: true
      topo:
        type: flat
```

Being the `clusterDefault`, it covers every partition, so no `topologyRef` is needed anywhere.

A mixed cluster declares both and binds the CPU partition explicitly:

```yaml
topology:
  topologies:
    - name: ib-gpu
      clusterDefault: true
      topo:
        type: block
        blockSizes: [8]
      nodeSetRefs: [h100]
    - name: cpu
      topo:
        type: flat
      nodeSetRefs: [worker-cpu]

partitionConfiguration:
  configType: structured
  partitions:
    - name: gpu
      nodeSetRefs: [h100]
      config: "Default=YES State=UP"
    - name: cpu
      nodeSetRefs: [worker-cpu]
      topologyRef: cpu
      config: "State=UP"
```

A flat topology only takes effect through a partition that reaches it, by `topologyRef` or by being
the `clusterDefault`. Declaring one and binding nothing to it leaves it inert in the file.

### Mixing nodes across topologies in one partition

This is the failure mode to design against, and it is not specific to CPU nodes. A block topology
refuses to allocate any node that is not in one of its blocks, and the check fires as soon as the
partition contains at least one node that *is* in a block:

```
srun: error: Unable to allocate resources: Requested node configuration is not available
```

with `requires nodes which are not in blocks` in the slurmctld log, and, at config load:

```
Blocks lack access to N nodes: worker-ephemeral-[0-15]
```

So a partition whose topology is a block topology must have every one of its nodes inside that
topology's blocks. Slurm's own guidance is to separate such nodes by partition. In practice that
means an all-nodes partition like `main` cannot use a block topology unless every NodeSet it spans
is described by it — bind it to a `flat` or `tree` topology instead, or narrow the partition.

Check it with:

```
scontrol show partition <name> | grep -i topology     # empty means it uses the clusterDefault
scontrol show topology <that topology>                # do all the partition's nodes appear?
```

## How topologies are built

Topologies are built from the `topology.nebius.com/tier-*` labels on the Kubernetes nodes (the
prefix defaults to `topology.nebius.com` and is configurable through the operator's
`TOPOLOGY_LABEL_PREFIX`). `NodeTopologyReconciler` collects them into the `topology-node-labels`
ConfigMap, and the plugin type decides which labels a topology reads:

- `block` groups nodes by `tier-0`, which is the NVL domain of the rack on GBX00 hardware;
- `tree` uses all present `tier-N` labels in numeric order, highest tier closest to the root.
  When present, `tier-0` is the leaf switch directly above the workers. Missing tier numbers are
  skipped; no hardware-specific configuration is needed.

Workers join their topology as part of slurmd registration, and the unit they pick is the one the
rendered config places them in: the lowest available tier's switch for a tree, the `tier-0` block
for a block topology. A node covered by several topologies joins all of them at once:

```
slurmd --conf "topology=ib-gpu:nvl0,eth-cpu:root:leaf01"
```

The init container resolves the value: it waits for the operator to publish a topology placing this
worker, then leaves the specification on the volume it shares with the slurmd container, which
passes it to `slurmd --conf`. Registration carries it, rather than a `scontrol update` issued
beforehand, because such an update moves the node into a state where slurmctld discards the
`InstanceId` and `Extra` that registration brings with it. Resolving it in the init container also
keeps the wait off slurmd's own startup.

Nodes with no usable labels, and nodes whose pods are not scheduled yet, land in a catch-all
`unknown` unit per fabric, so the topology stays complete across the pod lifecycle. That is why a
freshly scaled NodeSet shows up under `unknown` before its pods are placed.

## Verifying the result

Check which file the operator produced:

```
kubectl -n <ns> get cm <cluster>-topology-config -o jsonpath='{.data}' | jq 'keys'
```

The key is `topology.yaml`; no ConfigMap at all means the cluster declares no topologies.

Read the rendered topologies:

```
kubectl -n <ns> get cm <cluster>-topology-config -o jsonpath='{.data.topology\.yaml}'
```

From a login node, check what Slurm actually loaded and where a node and a partition ended up:

```
scontrol show topoconf              # every topology slurmctld parsed
scontrol show topology <name>       # one topology by name
scontrol show node h100-0 | grep -i topology
scontrol show partition gpu | grep -i topology
```

Use `topoconf` to check the whole file. `scontrol show topology` without a name prints only the
topology marked `cluster_default`, not all of them, and it takes a topology name rather than a
`--all` flag — so a missing topology there is usually the command, not the config.

The units in `scontrol show node` must exist in the rendered file. If a node reports a switch or
block that the file does not define, the worker and the operator disagree — see below.

### Checking that a tree really constrains scheduling

The file, the loaded config and the registrations can all agree while the scheduler ignores the
tree, and `$SLURM_TOPOLOGY_ADDR` no longer answers that question: on a multi-topology cluster it
reports the bare node name with `SLURM_TOPOLOGY_ADDR_PATTERN=node`, whatever topology the job's
partition uses.

What does answer it is `--switches`, which caps the leaf switches an allocation may span. Take two
workers that `scontrol show node` places on different leaf switches and ask for both within one
switch:

```
srun -p gpu -N2 -w h100-0,h100-4 hostname                 # allocates
srun -p gpu -N2 -w h100-0,h100-4 --switches=1 hostname    # waits, never placed
```

The first run is what tells a topology constraint apart from workers that are merely busy. Two
workers on the same leaf switch stay placeable under `--switches=1`.

### Acceptance scenarios

Every scenario opens with a step that reads the cluster's configuration. A cluster that does not
ask for the capability under test skips immediately, before any work; past that step a missing,
unhealthy or unusable resource fails.

`e2e/acceptance/features/topology.feature` runs in every e2e run and covers the published config,
what `topoconf` loaded, the partition bindings, the workers' registrations and a job per topology.

`e2e/acceptance/features/topology_tree.feature` covers the `--switches` behaviour and also runs in
every e2e run. It skips itself unless the cluster configures a tree spanning more than one leaf
switch.

`e2e/acceptance/features/topology_block.feature` holds every block topology scenario: task ranks
following the rendered block order, and scheduling after the base block size changes. Both are
tagged `@unstable`, so the default suite skips them: they need a cluster with a block topology, and
the resize scenario reconfigures it while it runs. They run with `--run-unstable`; to run only them:

```
go run ./e2e/cmd/acceptance --kubectl-context <ctx> --run-unstable --scenario features/topology_block.feature
```

`e2e/acceptance/features/topology_legacy.feature` checks the single `topology/tree` configuration
that clusters used before named topologies. It is tagged `>=4.0.0,<5.0.0`, so the runner picks it
only on those clusters and skips it on 5.0.0 and later.

### When Slurm re-reads the topology

Two different mechanisms keep the running cluster in step with the file, and it is worth knowing
which one is doing the work.

A node moving between switches travels with its own slurmd registration, which is what a
rescheduled pod performs on startup anyway. No re-read is involved, and none is wanted: re-reading
the file across the cluster because one pod moved would be a poor trade.

A structural change cannot be learned that way, so the operator asks sconfigcontroller for a
`scontrol reconfigure`. The request is recorded in the topology `JailedConfig` as a `Reconfigure`
update action, and withdrawn only once a reconfigure has demonstrably run over the new content.

#### What counts as a structural change

The operator compares the new file with the published one and sorts the difference by what
slurmctld would have to re-read to learn it:

| Reconfigures at once | Reconfigures after a quiet window | Needs no reconfigure |
| --- | --- | --- |
| a topology added or removed | leaf switches reordered, appearing or disappearing | a node moving between existing switches or blocks |
| a topology renamed or `topo.type` changed | blocks reordered or disappearing | an inner tree switch appearing, disappearing or renamed |
| `topo.blockSizes` changed | | the order inside a `children` list |
| `clusterDefault` moved to another topology | | |
| a block appearing | | |

Node membership travels with the worker's own slurmd registration. The order of leaf switches and
blocks decides task ranks, so slurmctld has to re-read it, but a stale order only worsens placement
and a leaf or block that disappeared stays loaded but harmless, so such changes can wait. A new block
cannot: a node registering into a block slurmctld has not loaded is set to `INVAL` and drained with
reason `Failed to set topology`. Scaling a NodeSet or changing `nodeSetRefs` falls into whichever
column the resulting file does.

#### Confirming a reconfigure

Each `JailedConfig` records the hash of what it last applied in `status.appliedHash`, so a
reconciliation over identical content does not reconfigure the cluster again. A request over content
that renders identically — a topology gaining an entry that reaches no node yet — is still honoured,
because raising it changes the JailedConfig generation.

The operator publishes the new config before raising the request, so a request only ever stands over
content already in the ConfigMap. The request is withdrawn once the `ReconfigurePerformed` condition
names the current generation, or, for a request raised while another was still outstanding so the
generation did not move, once `status.appliedHash` has moved past the value recorded when it was
raised. The withdrawal happens before any further content is published: a request left in the spec
would turn the next node movement into another reconfigure.

#### Deferred reconfigures for reordered leaves and blocks

`scontrol reconfigure` resets slurmctld's internal timers, so two reconfigures closer together than
the longest timeout in `slurm.conf` keep that timeout from ever expiring. Changes that only reorder
leaf switches or blocks are what powering racks down and up in waves produces, and on a large
cluster each one would otherwise restart slurmd on every node.

Such a change is published at once, so the file in the jail is always current, but the request is
held. It is raised once a cooldown has passed since the last reconfigure the operator caused and the
order has stopped changing for a debounce, though never later than a cap after the first held
change. Repeated reorders inside one window collapse into a single reconfigure, and a change that
needs an immediate reconfigure settles the held one along the way.

The deferral state lives in annotations on the topology `JailedConfig`, so an operator restart or a
leader change loses nothing. It is configured with operator flags:

| Flag | Default | Meaning |
| --- | --- | --- |
| `--topology-reconfigure-cooldown` | `15m` | Least time between the previous reconfigure and a deferred one; `0` disables it |
| `--topology-reconfigure-debounce` | `1m` | How long the order has to stay unchanged before the deferred reconfigure runs |
| `--topology-reconfigure-debounce-cap` | `15m` | Most a deferred reconfigure waits for the order to settle; `0` disables the cap |

Set them through `controllerManager.manager.args` in the `soperator` Helm chart.

#### Watching it happen

Every performed reconfigure is recorded as a Kubernetes event on each `JailedConfig` it covered, so
it is visible without reading operator logs:

```
kubectl -n <ns> describe jailedconfig <cluster>-topology-config
...
Events:
  Type    Reason         Age   From                    Message
  Normal  Reconfigured   1m    jailedconfig-controller slurmctld re-read the configs written for this JailedConfig
```

A failed one is recorded as a `Warning` with reason `ReconfigureFailed` and the error.

#### Misconfigurations reported as events

Three situations render a working config and fail only later, at scheduling time. The operator
reports each of them on the SlurmCluster, so `kubectl -n <ns> describe slurmcluster <name>` shows
them without digging through operator logs:

| Reason | Meaning |
| --- | --- |
| `TopologyReachesNoNode` | A `tree` or `block` topology matches no node and is rendered empty. Usually its `nodeSetRefs` names a NodeSet that does not exist, or only CPU-only ones. Not reported for `flat`, which lists no nodes by design. |
| `UnresolvedTopologyRef` | A partition's `topologyRef` names a topology that is not in the rendered config. The binding is dropped and the partition falls back to the cluster default. |
| `ClusterDefaultConflict` | Several topologies set `clusterDefault`. The first one stays the default, the rest are cleared. |

#### What a reconfigure does not do

It does not move already-registered nodes between topologies. Re-reading the file lays the nodes out
as written, but slurmctld then applies each node's own registered topology on top, and treats it as
the complete list: a node is removed from every topology its registration does not name. Workers
compute that value once, in their init container.

So a node that was running before a topology was added stays out of it until its pod restarts, no
matter how many times the cluster is reconfigured. Adding a topology and expecting existing nodes to
join it is the one case where the file and the running cluster legitimately disagree — check
`scontrol show node <name> | grep -i topology` against the file before assuming the config failed to
arrive.

## Troubleshooting

| Symptom | Cause | Fix |
| --- | --- | --- |
| No topology ConfigMap at all | The cluster declares no `spec.topology.topologies` | Declare them; nothing else describes a topology |
| A topology is missing from the file | Its `nodeSetRefs` match no existing NodeSet | Check the NodeSet names; look for the "Topology matches no NodeSet" log line |
| `Topology=` missing from a partition line | `topologyRef` names a topology the config does not declare | Look for the `WARNING: Partition ...` comment in `slurm.conf` |
| All nodes sit under `unknown` | Node topology labels are missing | Check the `topology.nebius.com/tier-*` labels on the Kubernetes nodes and the `topology-node-labels` ConfigMap |
| A block topology keeps its nodes in `unknown` while the tree looks right | The nodes carry no `tier-0` label, or it was added after the labels were last collected | `tier-0` is what a block groups by. Compare the node's labels with the `topology-node-labels` ConfigMap: the ConfigMap is materialized from the `topology-soperator` ResourceDistribution, so deleting it only restores the same content |
| A topology declared over CPU NodeSets is missing from the file | CPU-only NodeSets belong to the generated `cpu` topology, so it covers nothing | Bind the partition to `cpu` instead |
| Workers hang in init | The topology file has not been delivered, or does not yet list the worker | The init container waits for the hostname to appear in the file; check the ConfigMap and the `JailedConfig` |
