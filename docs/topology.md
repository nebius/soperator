# Network topology

Soperator describes the cluster's network to Slurm so the scheduler can place jobs close together on
the fabric. A cluster can be described either as one topology, or — since Slurm 25.05 — as several
named topologies that partitions bind to individually.

- [When you need several topologies](#when-you-need-several-topologies)
- [Which file gets rendered](#which-file-gets-rendered)
- [Quick start](#quick-start)
- [Configuration reference](#configuration-reference)
- [How topologies are built](#how-topologies-are-built)
- [Verifying the result](#verifying-the-result)
- [Troubleshooting](#troubleshooting)
- [Migrating from a single topology.conf](#migrating-from-a-single-topologyconf)

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

## Which file gets rendered

Slurm checks for `topology.yaml` first and ignores `topology.conf` when it exists, so Soperator
renders exactly one of them, never both.

| `SLURM_OPERATOR_ENABLE_MULTI_TOPOLOGY` | `spec.topology.topologies` | Result |
| --- | --- | --- |
| `false` | ignored | `/etc/slurm/topology.conf`, plugin from `spec.slurmConfig.topologyPlugin` |
| `true` (default) | set | `/etc/slurm/topology.yaml`, plugin per topology via `topo.type` |
| `true` (default) | empty | nothing rendered; reconciliation fails with an explicit error |

The flag alone picks the format. With it on, named topologies are the only source of topology
config, so a cluster that asks for a topology but defines none is rejected instead of quietly
falling back to `topology.conf`, which would leave no signal about why `topology.yaml` never
appeared. A cluster that asks for no topology at all — no named topologies and an empty
`spec.slurmConfig.topologyPlugin` — is left alone either way.

Both files are written to the `<cluster>-topology-config` ConfigMap by the topology controller and
mounted into the jail through a `JailedConfig`. Switching between modes drops the superseded key
from the ConfigMap.

## Quick start

The example below describes a cluster with two GPU NodeSets on InfiniBand and one CPU NodeSet on
Ethernet, and gives the GPU partition a block topology of its own.

### 1. Operator: keep the feature flag on

In the `soperator` chart it is on by default:

```yaml
controllerManager:
  manager:
    env:
      enableMultiTopology: "true"
```

Renders to `SLURM_OPERATOR_ENABLE_MULTI_TOPOLOGY` on the operator Deployment.

### 2. NodeSets: declare the fabric

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

### 3. SlurmCluster: define the topologies and bind the partitions

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

### 4. What comes out

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
| `topo.type` | yes | `tree` or `block`. |
| `topo.blockSizes` | no | Planning base block size followed by any higher-level sizes to enforce. Each successive value must be a power of two larger than the previous one. Only for `type: block`. |
| `clusterDefault` | no | Marks the topology used by partitions without a `topologyRef` and by cluster-wide operations not tied to a partition. |
| `nodeSetRefs` | no | NodeSets the topology covers. `ALL`, like an empty list, covers every GPU NodeSet; CPU-only ones are never covered, see below. |

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

A `nodeSetRefs` entry that matches no NodeSet is dropped, and a topology left with no NodeSet at all
is omitted from the file. A partition bound to such a topology loses its `Topology=` binding too, so
`slurm.conf` never points slurmctld at a topology the topology config does not define. Dangling
references turn up routinely while a SlurmCluster and its NodeSets are applied one after another,
and one stale name must not take the whole config down.

### `spec.partitionConfiguration.partitions[].topologyRef`

Available for `configType: structured`. Renders `Topology=<name>` on the partition line, mapping to
the `Topology` partition option of `slurm.conf`: *"Name of the topology, defined in topology.yaml,
used by jobs in this partition."* When empty, the partition uses the topology marked
`clusterDefault`.

The binding is dropped, with a warning comment in `slurm.conf` rather than a failed render, when the
ref does not reach a topology that is actually in the rendered config:

```
#WARNING: Partition typo references undefined topology nope, ignoring it
#WARNING: Partition spooky references topology ghost, which covers no NodeSet and is absent from the topology config, ignoring it
```

### Deprecated: `spec.topology.blockSize`

Configures the single `topology.conf` together with `spec.slurmConfig.topologyPlugin`. It still
works while the feature flag is off and will be removed in a future release; `topo.blockSizes` on a
named topology replaces it.

### The generated CPU-only topology

CPU-only NodeSets — those with `gpu.enabled: false` — sit on no fabric, so there is nothing for a
tree or a block to optimize. Placing them in one anyway would put them on a fabricated `unknown`
switch beside real IB leaves, telling the scheduler they are one hop from the GPU nodes.

Instead, whenever the cluster has at least one CPU-only NodeSet, the operator appends a flat
topology named `cpu`:

```yaml
- topology: ib-gpu
  cluster_default: true
  block:
    blocks:
        - block: nvl0
          nodes: h100-[0-7]
- topology: cpu
  cluster_default: false
  flat: true
```

Consequences worth knowing:

- CPU-only NodeSets are excluded from every user-defined topology, `nodeSetRefs: ALL` included.
  `ALL` means every GPU NodeSet.
- A partition of CPU nodes binds to it like any other: `topologyRef: cpu`.
- The entry goes last and is never the cluster default, so cluster-wide operations keep following a
  real fabric. The exception is a cluster with no GPU NodeSets at all: every user topology then
  covers nothing and drops out, leaving the flat one to become the default on its own.
- `flat` is not selectable through `topo.type`; only the operator generates it.
- If a configured topology is already named `cpu`, it wins and nothing is generated. The operator
  logs this, and CPU-only NodeSets then follow whatever that topology says.

Workers of a CPU-only NodeSet skip topology registration entirely — a flat topology defines no
units to register into — but still wait for the topology config to be delivered before `slurmd`
starts.

## How topologies are built

Topologies are built from the `topologyconf.slurm.nebius.ai/tier-*` labels on the Kubernetes nodes
(the prefix is configurable through the operator's `TOPOLOGY_LABEL_PREFIX`). `NodeTopologyReconciler`
collects them into the `topology-node-labels` ConfigMap, and the plugin type decides which labels a
topology reads:

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

`["topology.yaml"]` means multi-topology is active. `["topology.conf"]` means the legacy path is.

Read the rendered topologies:

```
kubectl -n <ns> get cm <cluster>-topology-config -o jsonpath='{.data.topology\.yaml}'
```

From a login node, check what Slurm actually loaded and where a node and a partition ended up:

```
scontrol show topology
scontrol show node h100-0 | grep -i topology
scontrol show partition gpu | grep -i topology
```

The units in `scontrol show node` must exist in the rendered file. If a node reports a switch or
block that the file does not define, the worker and the operator disagree — see below.

Note that the topology `JailedConfig` carries no `Reconfigure` update action, so a changed topology
file is materialized into the jail but does not by itself make slurmctld re-read it. Workers pick up
their own placement through `scontrol update` at startup; a controller-side reload happens on the
next `scontrol reconfigure` or slurmctld restart.

## Troubleshooting

| Symptom | Cause | Fix |
| --- | --- | --- |
| Operator logs `SLURM_OPERATOR_ENABLE_MULTI_TOPOLOGY is enabled but spec.topology.topologies is empty` | The flag is on and the cluster uses a topology but defines none | Add `spec.topology.topologies`, or set `enableMultiTopology: "false"` |
| ConfigMap still has `topology.conf` and no error is logged | The running operator predates the feature | Check the Deployment image actually contains it |
| ConfigMap has neither key | The cluster asks for no topology at all | Set `spec.slurmConfig.topologyPlugin` or define topologies |
| A topology is missing from the file | Its `nodeSetRefs` match no existing NodeSet | Check the NodeSet names; look for the "Topology matches no NodeSet" log line |
| `Topology=` missing from a partition line | `topologyRef` names an undefined topology, or one that covers no NodeSet and is therefore absent from the topology config | Look for the `WARNING: Partition ...` comments in `slurm.conf` |
| All nodes sit under `unknown` | Node topology labels are missing | Check `topologyconf.slurm.nebius.ai/tier-*` labels and the `topology-node-labels` ConfigMap |
| A topology declared over CPU NodeSets is missing from the file | CPU-only NodeSets belong to the generated `cpu` topology, so it covers nothing | Bind the partition to `cpu` instead |
| Workers hang in init | The topology file has not been delivered, or does not yet list the worker | The init container waits for the hostname to appear in the file; check the ConfigMap and the `JailedConfig` |

## Migrating from a single topology.conf

1. Before upgrading the operator, either set `enableMultiTopology: "false"` to stay on
   `topology.conf`, or prepare the `spec.topology.topologies` of step 2 to apply together with the
   upgrade. The flag defaults to on, so a cluster that uses a topology and defines none will fail
   reconciliation after the upgrade.
2. Add `spec.topology.topologies` with one entry whose `topo.type` matches your current
   `spec.slurmConfig.topologyPlugin`, `clusterDefault: true`, and no `nodeSetRefs` (or `ALL`). This
   is equivalent to the single topology you had, now expressed in `topology.yaml`:

   ```yaml
   # Before
   slurmConfig:
     topologyPlugin: "topology/block"
   topology:
     blockSize: 18

   # After
   topology:
     topologies:
       - name: default
         clusterDefault: true
         topo:
           type: block
           blockSizes: [18]
   ```

3. Split the fabrics into further entries and point partitions at them with `topologyRef`.
4. Drop `spec.topology.blockSize` once every topology carries its own `topo.blockSizes`.

Rolling back is the reverse: setting `enableMultiTopology: "false"` restores `topology.conf` on the
next reconciliation, and the superseded `topology.yaml` key is dropped from the ConfigMap.

During the switch, workers wait for whichever of the two files is present before starting `slurmd`,
so a node never registers against a topology that has not been delivered yet.
