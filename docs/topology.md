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

## Configuration reference

### `spec.topology.topologies[]`

| Field | Required | Meaning |
| --- | --- | --- |
| `name` | yes | Identifies the topology. Partitions reference it, and workers register into it with `scontrol update ... Topology=<name>:<unit>`. |
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
- `tree` walks the contiguous `tier-1`..`tier-N` chain, highest tier closest to the root. `tier-0`
  names a block rather than a switch and is left out of the tree.

Workers register themselves into their topology with `scontrol update ... Topology=<name>:<unit>`,
and the unit they pick is the one the rendered config places them in: the `tier-1` switch for a
tree, the `tier-0` block for a block topology. A node covered by several topologies registers into
all of them at once:

```
scontrol update NodeName=h100-0 Topology=ib-gpu:nvl0,eth-cpu:root:leaf01
```

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

`e2e/acceptance/features/topology_block.feature` is kept out of the default suite because it
reconfigures the block topology while it runs. Start it by hand:

```
go run ./e2e/cmd/acceptance --kubectl-context <ctx> --scenario features/topology_block.feature
```

`e2e/acceptance/features/topology_legacy.feature` checks the single `topology/tree` configuration
that clusters used before named topologies. It is tagged `>=4.0.0,<5.0.0`, so the runner picks it
only on those clusters and skips it on 5.0.0 and later.

### When Slurm re-reads the topology

Two different mechanisms keep the running cluster in step with the file, and it is worth knowing
which one is doing the work.

A node moving between switches is pushed straight into the running slurmctld by the worker itself
with `scontrol update ... Topology=`. No re-read is involved, and none is wanted: restarting slurmd
across the cluster because one pod was rescheduled would be a poor trade.

A structural change cannot be learned that way, so the operator asks sconfigcontroller for a
`scontrol reconfigure`. The request is recorded in the topology `JailedConfig` as a `Reconfigure`
update action, and withdrawn only once a reconfigure has demonstrably run over the new content.

#### What counts as a structural change

The operator reduces the rendered file to a structure fingerprint, keeping only what slurmctld can
learn by re-reading it: for each entry, its name, its plugin, its `block_sizes` and its
`cluster_default` flag. For the example above that is:

```
flat=flat:[]:true,tree-ib=tree:[]:false,block-nvl72=block:[18]:false
```

| Asks for a reconfigure | Does not |
| --- | --- |
| a topology added or removed | a node moving between switches or blocks |
| a topology renamed | a switch or block appearing, disappearing or renamed |
| `topo.type` changed | `nodeSetRefs` changed |
| `topo.blockSizes` changed | a NodeSet scaled up or down |
| `clusterDefault` moved to another topology | |
| topologies reordered in the spec | |

The right-hand column is deliberate: those are node membership changes, and membership travels
through the worker's own `scontrol update`, not through a cluster-wide re-read.

#### Two further gates

A reconfigure also has to be worth doing. Each `JailedConfig` records the hash of what it last
applied in `status.appliedHash`, so a reconciliation over identical content does not reconfigure the
cluster again. A request over content that renders identically — a topology gaining an entry that
reaches no node yet — is still honoured, because otherwise it would never be satisfied.

The operator publishes the new config before raising the request, so a request only ever stands over
content already in the ConfigMap. Withdrawal is keyed on `status.appliedHash` moving past the value
recorded when the request was raised, rather than on the JailedConfig generation: the request lives
in the spec while the structure lives in an annotation, so a second structural change arriving while
a request is outstanding leaves the generation untouched, and a confirmation earned over the previous
content would otherwise read as confirming the new one.

#### Watching it happen

Every published change to `topology.yaml` is recorded on the SlurmCluster, in its own namespace and
under its own name, with a summary of how node membership moved:

```
kubectl -n <ns> describe slurmcluster <name>
...
Events:
  Type    Reason             Age   From                     Message
  Normal  TopologyRendered   2m    workertopology-reconciler Published topology.yaml: +block-nvl72 (2 nodes); tree-ib 6 nodes (+0 -2)
```

This is the only signal for a node moved between topologies: membership is not part of the structure
fingerprint, so such an edit asks for no reconfigure. The operator log carries the same change with
the node names spelled out, under `Topology config changed, publishing it`.

Every performed reconfigure is recorded as a Kubernetes event on each `JailedConfig` it covered, so
it is visible without reading operator logs:

```
kubectl -n <ns> describe jailedconfig <cluster>-topology-config
...
Events:
  Type    Reason         Age   From                    Message
  Normal  Reconfigured   1m    jailedconfig-controller slurmctld re-read the configs written for this JailedConfig
```

A failed one is recorded as a `Warning` with reason `ReconfigureFailed`, naming the config that
triggered the reconfigure, why it was due, and the error. When the failure is nodes not coming back,
the error names them:

```
nodes did not restart within 5m0s: worker-[3-5]: context deadline exceeded
```

Repeated failures aggregate into one event with a count, so `kubectl get events` shows how long a
request has been failing without comparing timestamps by hand.

#### Misconfigurations reported as events

Three situations render a working config and fail only later, at scheduling time. The operator
reports each of them on the SlurmCluster, so `kubectl -n <ns> describe slurmcluster <name>` shows
them without digging through operator logs:

| Reason | Meaning |
| --- | --- |
| `TopologyReachesNoNode` | A `tree` or `block` topology matches no node and is rendered empty. Usually its `nodeSetRefs` names a NodeSet that does not exist, or only CPU-only ones. Not reported for `flat`, which lists no nodes by design. |
| `UnresolvedTopologyRef` | A partition's `topologyRef` names a topology that is not in the rendered config. The binding is dropped and the partition falls back to the cluster default. |
| `ClusterDefaultConflict` | Several topologies set `clusterDefault`. The first one stays the default, the rest are cleared. |

`TopologyRendered` is emitted on the same object but is not a problem report: it records a normal
publish.

#### What a reconfigure does not do

It does not move already-registered nodes between topologies. Re-reading the file lays the nodes out
as written, but slurmctld then applies each node's own `Topology=` registration on top, and treats
that registration as the complete list: a node is removed from every topology its registration does
not name.

### How a node's registration is kept correct

The `Topology=` field of `scontrol show node` is the node's own registration, not a view of the
file. Two things maintain it, and they divide the work:

- The worker registers itself when its pod starts, from its init container. This is the fast path,
  and it is the only one that runs before slurmd does.
- The operator converges it. On every reconcile it compares the rendered config against what
  slurmctld reports and corrects the difference, so a registration that was lost or is out of date
  heals within a reconcile.

The converging half exists because the fast path is fragile in one specific way: a worker registers
before its slurmd comes up, which means Slurm still considers the node powered down. If a
reconfigure lands in that window, slurmctld discards the registration while restoring node state —
it deliberately does not preserve dynamic topology for a powered-down node — and nothing in the pod
recomputes it. That is a race a worker cannot win on its own, and it is why the file alone is not
enough either: a node's own registration overrides whatever the file says about it.

The push is cheap by construction. The desired side comes from the file, the current side from the
node cache the operator already refreshes, and nodes needing the same value are sent as one hostlist
request — so a steady cluster costs no request at all, and a re-render costs one request per changed
switch rather than one per node. It never triggers a reconfigure: `scontrol update` changes the live
tree in place.

Two cases are deliberately left alone. Nodes that are powered down are skipped, because a
registration written to them would be discarded exactly as described above; they are picked up once
they come up. Nodes the config places in the catch-all `unknown` unit are skipped too, since that
unit means the placement is not known yet, and asserting it into slurmctld would replace "no answer"
with a wrong one.

| Reason | Meaning |
| --- | --- |
| `NodeTopologyPushed` | The operator corrected one or more registrations to match the config. |
| `NodeTopologyPushFailed` | A registration could not be written. The note names the nodes, the value and the error. |

A push waits until the reconfigure for the current structure has been confirmed. Registering a node
into a topology slurmctld has not read yet fails for every node in it, because the set of named
topologies is fixed when the process starts and only a reconfigure — which re-execs slurmctld — can
extend it.

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
