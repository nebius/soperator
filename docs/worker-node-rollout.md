# Slurm-aware worker and node rollout

Set `spec.updateStrategy: slurmAwareRollingUpdate` on a NodeSet to coordinate worker pod updates and voluntary
Kubernetes node eviction with Slurm. For the `nodesets` Helm chart, set `nodesets[].updateStrategy`:

```yaml
nodesets:
  - name: worker-gpu
    updateStrategy: slurmAwareRollingUpdate
    maxUnavailable: 1
```

When the `rollingupdate` controller is enabled, the NodeSet controller creates a `policy/v1` PodDisruptionBudget (PDB)
before reconciling its worker StatefulSet.
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

Cleanup of recovered workers continues while other workers are still rolling out or waiting for eviction. It uses
an up-to-date, Ready pod outside the replacement set and shares the same Slurm node list as active handoffs.

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
Disabling the `rollingupdate` controller (for example, `--controllers=*,-rollingupdate`) also removes these PDBs
and prevents their creation, provided the NodeSet controller remains enabled. Re-enabling `rollingupdate` recreates
them for NodeSets using `slurmAwareRollingUpdate`. PDBs owned by other resources are left untouched.
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

## Metrics and dashboard

The operator exposes rollout gauges on its metrics endpoint for worker StatefulSets managed by the rolling update
controller. They describe both worker revision updates and cordon-driven node replacements. Values are refreshed
after each reconciliation, so they reflect the last controller observation rather than a live stream of pod changes.
For idle NodeSets, Slurm information follows the audit interval described above.

The `soperator_rollout_` prefix identifies the application and the measured rollout state. All six metric families
explicitly attach the fixed label `controller="rollingupdate"`, using the same controller name as controller-runtime.
Controller-runtime does not add this label to custom metrics automatically; its own controller metrics already have
it. Workqueue metrics use `name="rollingupdate"`. Resource identity, worker stage and wait reason are also labels.
The rollout metrics are gauges, so they do not use the counter suffix `_total`; the progress timestamp uses seconds.

Update the operator image and dashboard together, and update any custom queries to use the metric names below and
`controller="rollingupdate"`. Historical samples with earlier names or without the controller label remain separate
series and do not match the new dashboard filters.

The [Soperator / Rollouts dashboard](../helm/soperator-monitoring-dashboards/dashboards/operator_rollouts.json)
shows worker counts by stage, progress and the dominant controller blocker per NodeSet. Its worker-stage panel uses
separate lines with points; stages can be isolated through the legend. Filter by namespace, SlurmCluster and NodeSet
to inspect a particular rollout. Prometheus must scrape the operator's metrics endpoint; the `soperator` chart
provides a ServiceMonitor when `serviceMonitor.enabled` is set.

### Labels and cardinality

All rollout metrics use the same controller and resource labels:

| Label | Meaning |
| --- | --- |
| `controller` | Fixed value `rollingupdate`, matching the name registered with controller-runtime. |
| `resource_namespace` | Namespace of the worker StatefulSet, which can differ from the operator's namespace. |
| `slurm_cluster` | SlurmCluster name. |
| `nodeset` | NodeSet name; falls back to the StatefulSet name if NodeSet identity is unavailable. |

`soperator_rollout_waiting` also has a `reason` label, and
`soperator_rollout_workers` has a `stage` label. Both use fixed vocabularies listed below.
There are no per-pod, node, UID, revision or error-message labels. Each observed worker StatefulSet (normally one per
NodeSet) contributes 27 series per scrape target: four single-series gauges, ten wait reasons and thirteen worker
stages, including zero-valued series.
This count does not grow with the number of workers; the fixed `controller` value does not multiply it. Prometheus
may add target labels such as `job`, `instance` and the physical `cluster`; these are separate from the labels above.

### Rollout gauges

| Metric | Meaning |
| --- | --- |
| `soperator_rollout_active` | `1` while revision updates, readiness, replacement handoffs or Slurm cleanup remain incomplete; `0` after completion is confirmed. A cordon-driven replacement can be active with no outdated pods. |
| `soperator_rollout_outdated_pods` | Number of observed pods outside the target revision; zero if that revision is not yet known. Zero does not imply that all replacements are Ready or that Slurm cleanup has finished. |
| `soperator_rollout_available_handoff_slots` | Remaining budget for starting additional handoffs after this pass's reservations. It is clamped to zero and remains populated when no rollout is active. Observation failures report zero. |
| `soperator_rollout_last_progress_timestamp_seconds` | Unix timestamp of the start of the current observed rollout or its last observed progress. Interpret it only while `soperator_rollout_active == 1` and the timestamp is greater than zero. |
| `soperator_rollout_waiting{reason}` | `1` for one dominant controller wait reason; all other reasons are `0`. All reasons are zero after a successful completed observation. This is a blocker indicator, not a worker count. |
| `soperator_rollout_workers{stage}` | Number of workers in each mutually exclusive rollout stage, plus a separate count of missing desired pod slots. |

Available slots follow the shared `maxUnavailable` budget described above. With an idle, fully Ready NodeSet and
up-to-date StatefulSet status, this is the full configured budget, capped by the desired replica count. Zero can mean
that existing handoffs or unavailable replicas consume the budget, that desired replicas are zero, or that an
observation failed. A lagging StatefulSet Ready count can temporarily keep the value low. Check the worker stages
and controller blocker alongside it; zero slots does not mean that in-flight replacements have stopped progressing.

Progress includes reductions in outdated or pending workers, increasing readiness, handoff advancement, pod
replacement and clearing pending cleanup. Repeated polling alone does not advance the timestamp, and readiness
flapping does not repeatedly count as progress. A changed target revision or desired replica count starts a new
progress window while rollout is incomplete. Progress tracking is held in memory: operator restarts, StatefulSet
recreation or resource-label changes reset it. It is not the historical start time or total duration of a rollout.

### Worker stages

Each observed pod is counted once, including terminating pods. `waiting_for_pod` counts desired worker slots without
a pod: `max(desired replicas - observed pods, 0)`. With a complete observation, summing all stages gives
`max(desired replicas, observed pods)`. A terminating pod and its missing replacement are not counted twice.

| `stage` | Meaning |
| --- | --- |
| `ready` | Pod is Ready at the target revision, with no known replacement handoff or rollout-related Slurm restoration pending. This does not assert general Slurm schedulability. |
| `waiting_for_slot` | Replacement has not started because the concurrent update budget is exhausted. |
| `waiting_for_jobs` | Handoff is requested and Slurm reports allocated CPU or memory, or jobs in `COMPLETING`. A reboot flag alone is not evidence of running jobs. |
| `waiting_for_slurm` | Handoff is requested, no job-allocation/completion evidence was observed, and Slurm has not issued worker shutdown. |
| `stopping_worker` | Slurm has issued the reboot; the controller is waiting for the worker's handoff acknowledgement. |
| `waiting_for_eviction` | A cordoned worker has acknowledged its handoff and is waiting for the external node drainer to evict its pod. |
| `deleting_pod` | Pod deletion was accepted or the pod is terminating. It remains counted until a later observation sees it disappear. |
| `starting_pod` | A target-revision pod exists but is not Ready yet. |
| `waiting_for_pod` | A desired worker slot has no pod yet. This is a missing slot, not an existing pod. |
| `restoring_slurm` | Rollout-related drain or reboot state needs cleanup or confirmation from Slurm. |
| `missing_slurm_node` | A successful Slurm node listing did not contain the worker. |
| `blocked` | Replacement is not yet safe, or an action for this worker failed. Check the controller blocker and operator logs. |
| `unknown` | The worker's state could not be confirmed, for example after an API failure or an interrupted observation. |

On partial failures, already observed worker stages are retained for that pass and unconfirmed workers can be
`unknown`. If the pod list is unavailable, the last-known or desired worker capacity is reported as `unknown`;
a failed StatefulSet read uses the last-known capacity. This is not a fresh pod count.

### Controller wait reasons

Several worker stages can coexist, but only one dominant `reason` is exported at a time. Errors and unsafe states
take precedence over ordinary waits. In particular, `budget_exhausted` can coexist with workers finishing jobs,
stopping or starting new pods; use the stage counts to see what those workers are doing.

| `reason` | Meaning |
| --- | --- |
| `budget_exhausted` | No budget remains to start another queued handoff. |
| `reboot_pending` | A Slurm handoff has been requested but has not advanced to worker shutdown. |
| `handoff_pending` | Slurm has issued the reboot; worker shutdown acknowledgement is pending. |
| `pods_not_ready` | The desired pod revision/readiness state has not yet been confirmed. |
| `eviction_pending` | A cordoned worker has completed handoff and is waiting for external eviction. |
| `cleanup_pending` | Rollout-related Slurm state or worker registration still needs confirmation, including removal of stale rollout DRAIN when applicable. This is not filesystem cleanup. |
| `safety_pending` | The controller cannot safely use the offline-worker recovery path with the observed state and allocations. |
| `slurm_node_missing` | A worker needed for replacement is absent from a successful Slurm node listing. |
| `slurm_unavailable` | The Slurm client is unavailable, or a Slurm read/action failed. |
| `reconcile_error` | A Kubernetes operation or another reconciliation step failed. |

For example, `active=0`, `outdated_pods=0`, no wait reason and positive available slots describe an idle, completed
NodeSet with spare update budget. If a later API read fails, the previous active/progress values can remain while
slots become zero and an error reason is set; `active=0` alone is therefore not a health signal.

Series are removed when the controller observes the worker StatefulSet being deleted or coordination being
disabled; resource-label changes also remove the old series. Missing metrics mean no observation, not successful
completion. Check the scrape and dashboard filters
before interpreting missing data. The controller logs operational failures and returns a nil reconcile error to
preserve its polling interval, so `controller_runtime_reconcile_errors_total` can stay zero during those failures.
Use `soperator_rollout_waiting` and the operator logs for the affected namespace and StatefulSet.
