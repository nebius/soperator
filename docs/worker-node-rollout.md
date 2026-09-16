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

## Reconciliation interval

The rolling update controller runs a separate periodic loop for each enabled NodeSet, keyed by its worker
StatefulSet's namespace and name. The loop starts when the StatefulSet is first observed (including after an operator
restart), or when its strategy changes to `slurmAwareRollingUpdate`.

Each loop repeats with `--requeue-after-rolling-update` (default `1m`) after its reconciliation finishes. Configure this
flag through the operator arguments (`controllerManager.manager.args` in the `soperator` Helm chart). The interval is
the same when there is no work, after an undrain, and after errors. Errors are logged and do not stop the loop. Deleting
the StatefulSet or disabling coordination stops its loop; enabling coordination again restarts it.

The rolling update controller uses a two-minute timeout for each Slurm HTTP request, including retries and reading
the response body. A timed-out request ends the current pass, and the NodeSet is retried after the same configured
reconciliation interval. Other components retain their existing Slurm client timeout settings.

Idle NodeSets check Slurm on the first pass and then at `--rolling-update-idle-slurm-audit-interval` (default `15m`).
Set this positive duration through `controllerManager.manager.args`, for example
`--rolling-update-idle-slurm-audit-interval=30m`. The audit runs on the next regular reconciliation after the interval
expires; values shorter than the reconciliation interval cause an audit on every pass. The Kubernetes cache is still
checked on every pass, so cordon and revision changes start rollout without waiting for that audit. Changes to worker
pod UID, readiness, termination or membership also trigger a Slurm check on the next pass. Unrelated pod status updates do not trigger extra Slurm reads.

Pending cleanup is checked at the normal reconciliation interval. This includes rolling-update drains that are not
ready for UNDRAIN yet, such as `DOWN+DRAIN`, ongoing reboots, and drains on unready pods. A missing Slurm node keeps
cleanup unresolved. Successful UNDRAIN is confirmed by a subsequent read; errors and partially applied batches keep
unresolved workers pending without stopping other handoffs. Drains with another reason are not cleared.

Cleanup tracking is held in memory and rebuilt from current pods and Slurm after an operator restart, re-enabling
coordination, or recreating the StatefulSet. A removed pod stops being tracked; a new pod with the same name triggers
another check even if it is already Ready. The periodic idle audit catches changes that occur only in Slurm. Failed
checks retry at the normal reconciliation interval. Active rollout and pending cleanup still use full Slurm node lists
per NodeSet; this optimization reduces idle requests but does not share Slurm snapshots across NodeSets.

Pod and Node watches keep the shared cache up to date without enqueueing reconciliations. Updates to an already enabled
StatefulSet also do not enqueue extra reconciliations. Cordon changes and worker acknowledgements are observed on the
next periodic pass. Different NodeSets can be processed concurrently according to `--max-concurrent-reconciles`.

## Worker lifecycle

Both a worker template update and a Kubernetes node cordon use the same handoff. The worker StatefulSet uses
`OnDelete`, so replacing an outdated worker waits for this coordination. Recovery paths for failed containers
are described below.

`worker-operation-phase=ready` acknowledges that the old worker can be removed. It is separate from the Kubernetes
`PodReady` condition of its replacement. The node rollout tool owns eviction on cordoned nodes; Soperator waits for it.
If the node is uncordoned before eviction, Soperator deletes the acknowledged pod on a subsequent pass.

## Node drain

1. The node rollout tool cordons a Kubernetes node (`spec.unschedulable: true`) and requests eviction of its pods.
   The PDB blocks worker eviction. The next periodic scan detects the cordon, even when the worker's image is already current.
2. Soperator starts a worker operation and requests the existing Slurm `reboot ASAP` handoff. Slurm drains the
   worker and lets running jobs finish before invoking the handoff program.
3. The worker acknowledges its operation with `slurm.nebius.ai/worker-operation-phase: ready`.
   This phase removes the pod from the PDB selector, allowing the node drainer to retry eviction.
4. The node rollout tool retries eviction and can now remove the worker. Its StatefulSet creates a replacement
   pod on an eligible node. The replacement is protected by the PDB again.

An operation already in progress keeps its ID when the node is cordoned or the worker template changes. If cordon
is removed after handoff starts, Soperator finishes the operation and replaces the stopped pod itself. An operator
restart reconstructs this state from the pod labels and Slurm state.

On a cordoned node, a crash-looping init container or an offline slurmd does not bypass the handoff. The controller
requests the normal Slurm `reboot ASAP` operation and keeps PDB protection until the worker itself acknowledges
`phase=ready`. A snapshot with zero allocations does not establish readiness for a later eviction: the worker could
recover and accept jobs in the meantime. The same acknowledgement is required for an offline worker whose reboot
is already in progress.

If a failure prevents the worker from ever executing the handoff, node drain waits for recovery or manual intervention.
Direct `kubectl delete pod` remains available as an explicit bypass. On uncordoned nodes, the existing immediate-deletion
recovery paths remain: crash-looping worker initialization, or an eligible offline slurmd with zero known CPU and memory
allocations and no completing jobs.

## One reconciliation pass and the shared budget

Worker image updates and node rollout share the NodeSet's `maxUnavailable` budget. This is separate from the PDB's
`maxUnavailable: 0`: the controller limits concurrent handoffs, while the PDB gates eviction of individual workers.

Completing a handoff does not end the pass while other workers still need processing. A pod deleted in this pass still
consumes a slot: if it was `PodReady=True` in the snapshot, it is counted explicitly; otherwise it is already included in
unavailable replicas. Terminating pods are also counted as unavailable, without an extra charge.
Pods still handing off, or already released for eviction, continue consuming budget even if Kubernetes reports them
as Ready. Already unready workers on cordoned nodes can start a handoff even when there are no free slots.

If a worker awaiting handoff is missing from a successful Slurm node list response, the controller leaves it protected
and retries on the next periodic pass. It conservatively consumes one budget slot even if Kubernetes reports it Ready;
unready pods are already counted in unavailable replicas. Other workers continue within the remaining budget. With
`maxUnavailable: 1`, one missing Slurm node can therefore prevent new handoffs until it reappears. An error fetching the
Slurm node list still stops Slurm processing for the current pass.

If preparing a worker operation fails, including a pod version conflict, that worker is excluded from the reboot
batch while successfully prepared workers proceed. Its reserved budget slot remains occupied for the current pass
because the pod may have become unavailable since the snapshot. Errors are reported after attempting the reboot
batch, and failed workers are reconsidered on the next periodic pass without immediate patch retries.

Stale rolling-update drains are cleared with one batch Slurm `UNDRAIN` request per pass. A batch error is logged
without stopping independent handoffs; workers selected for undrain keep their budget slots for the current pass.
The request may have applied partially, so the next pass reads Slurm state again and selects only drains that still
match the cleanup criteria, including the rolling-update reason.

Slots become reusable as replacements become Ready and the cache and StatefulSet status reflect that progress.
There is no barrier between batches. For example, with 100 workers in one NodeSet and `maxUnavailable: 50`:

| Observed progress | Slots occupied before new handoffs | New handoffs allowed in this pass |
| --- | --- | --- |
| The first 50 workers are handing off or waiting for replacement | 50 | 0 |
| 10 replacements are Ready; 39 old workers are still handing off; one old pod is deleted in this pass | 40 | 10 |
| Those 10 new handoffs have started; the earlier 40 replacements are still pending | 50 | 0 |

The deleted pod in the middle row remains one of the 40 occupied slots. The controller can nevertheless start 10 new
handoffs immediately, using the slots freed by the replacements that are already Ready.

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
