# Slurm-aware worker and node rollout

Set `spec.updateStrategy: slurmAwareRollingUpdate` on a NodeSet to coordinate worker pod updates and voluntary
Kubernetes node eviction with Slurm. For the `nodesets` Helm chart, set `nodesets[].updateStrategy`:

```yaml
nodesets:
  - name: worker-gpu
    updateStrategy: slurmAwareRollingUpdate
    maxUnavailable: 1
```

The NodeSet controller creates a `policy/v1` PodDisruptionBudget (PDB) before reconciling its worker StatefulSet.
The PDB has the same namespace and name as the StatefulSet, uses `maxUnavailable: 0`, and selects this NodeSet's
workers unless they carry `slurm.nebius.ai/worker-operation-phase: ready`. Existing workers and replacement pods
are protected without needing a new label in the pod template. Running but unready workers use `IfHealthyBudget`
so a readiness failure alone does not allow their jobs to be interrupted.

## Node drain

1. The node rollout tool cordons a Kubernetes node (`spec.unschedulable: true`) and requests eviction of its pods.
   The PDB blocks worker eviction. Cordon itself is the trigger, even when the worker's image is already current.
2. Soperator starts a worker operation and requests the existing Slurm `reboot ASAP` handoff. Slurm drains the
   worker and lets running jobs finish before invoking the handoff program.
3. The worker acknowledges its operation with `slurm.nebius.ai/worker-operation-phase: ready`.
   This phase removes the pod from the PDB selector, allowing the node drainer to retry eviction.
4. The node rollout tool retries eviction and can now remove the worker. Its StatefulSet creates a replacement
   pod on an eligible node. The replacement is protected by the PDB again.

Worker image updates and node rollout share the NodeSet's `maxUnavailable` budget. Cordoned workers take priority
when choosing new operations. A worker released from the PDB still occupies its slot until eviction and replacement.
Already unready workers on cordoned nodes can finish their handoff without consuming another slot.

An operation already in progress keeps its ID when the node is cordoned or the worker template changes. If cordon
is removed after handoff starts, Soperator finishes the operation and replaces the stopped pod itself. An operator
restart reconstructs this state from the pod labels and Slurm state.

If worker initialization is crash-looping, there is no running slurmd to hand off and eviction is allowed. The existing
recovery path for a failed slurmd also allows completion when Slurm reports a safely offline worker with zero known
CPU and memory allocations and no completing jobs. On a cordoned node, these recovery paths set the operation ID
and `phase=ready` together. Missing allocation data or a Slurm API error keeps protection.

## Requirements and scope

Deploy the operator and verify the PDB exists before starting node rollout. The NodeGroup drain timeout must allow
running Slurm jobs to finish, ideally without a fixed limit. A timeout that forcibly removes a node cannot be made safe
by this PDB. Replacement workers also need eligible Kubernetes capacity.

This handles voluntary eviction through the Kubernetes Eviction API. Direct pod deletion, a drain using
`--disable-eviction`, and involuntary node loss do not honor PDB protection. Additional PDBs selecting the same workers
may continue to block eviction after Soperator releases its protection.

Switching a NodeSet back to `rollingUpdate` removes its Soperator-owned PDB and disables this coordination.
The PDB is also garbage-collected with its NodeSet.

## Inspect progress

```sh
kubectl get pdb -n <namespace>
kubectl get nodes
kubectl get pods -n <namespace> \
  -l slurm.nebius.ai/nodeset=<nodeset> \
  -L slurm.nebius.ai/worker-operation-id,slurm.nebius.ai/worker-operation-phase
```

During `stopping`, check the worker's jobs and reboot state in Slurm. After the operation phase becomes `ready`, check the
node rollout tool's eviction retries and any other matching PDBs. A merely cordoned node needs an actual drain to
remove a worker whose operation is `ready`.
