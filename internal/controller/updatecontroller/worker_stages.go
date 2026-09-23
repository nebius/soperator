package updatecontroller

import (
	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"

	"nebius.ai/slurm-operator/internal/slurmapi"
)

type workerStage uint8

const (
	workerReady workerStage = iota
	workerWaitingForSlot
	workerWaitingForJobs
	workerWaitingForSlurm
	workerStopping
	workerWaitingForEviction
	workerDeleting
	workerStarting
	workerWaitingForPod
	workerRestoringSlurm
	workerMissingSlurmNode
	workerBlocked
	workerUnknown
	workerStageCount
)

// Values of the stage label on rollout_workers, indexed by workerStage.
var workerStageLabelValues = [workerStageCount]string{
	"ready", "waiting_for_slot", "waiting_for_jobs", "waiting_for_slurm", "stopping_worker",
	"waiting_for_eviction", "deleting_pod", "starting_pod", "waiting_for_pod", "restoring_slurm",
	"missing_slurm_node", "blocked", "unknown",
}

type workerStageObservation struct {
	stage                             workerStage
	replacing, ready, hasJobs, cached bool
}

// Names exist only during one reconcile; exported and retained state is aggregate.
func (o *rolloutObservation) observeWorkerPods(pods []corev1.Pod) {
	if o == nil {
		return
	}
	o.workerPodsKnown = true
	o.workers = make(map[string]*workerStageObservation, len(pods))
	for _, pod := range pods {
		stage := workerUnknown
		if pod.DeletionTimestamp != nil {
			stage = workerDeleting
		}
		o.workers[pod.Name] = &workerStageObservation{stage: stage}
	}
}

func (o *rolloutObservation) observeWorkerPlan(sts *kruisev1b1.StatefulSet, pods []corev1.Pod, replacements []workerReplacement) {
	if o == nil {
		return
	}
	for _, replacement := range replacements {
		if worker := o.workers[replacement.pod.Name]; worker != nil {
			worker.replacing = true
		}
	}
	for _, pod := range pods {
		worker := o.workers[pod.Name]
		if worker == nil || worker.replacing || worker.stage == workerDeleting {
			continue
		}
		if sts.Status.ObservedGeneration >= sts.Generation && sts.Status.UpdateRevision != "" && pod.Labels["controller-revision-hash"] == sts.Status.UpdateRevision {
			worker.ready = podReady(&pod)
			if !worker.ready {
				worker.stage = workerStarting
			}
		}
	}
}

func (o *rolloutObservation) setWorkerStage(name string, stage workerStage) {
	if o == nil {
		return
	}
	if worker := o.workers[name]; worker != nil {
		worker.stage, worker.cached = stage, false
	}
}

func (o *rolloutObservation) observeWorkerSlurm(nodes []slurmapi.Node) {
	if o == nil {
		return
	}
	o.workerSlurmRead = true
	// Only workers outside the replacement plan need the cleanup classification.
	// Replacement stages come from the controller's actual per-worker decisions.
	for _, worker := range o.workers {
		if !worker.replacing && worker.ready {
			worker.stage = workerMissingSlurmNode
		}
	}
	for _, node := range nodes {
		worker := o.workers[node.Name]
		if worker == nil || worker.replacing || !worker.ready {
			continue
		}
		if hasRollingUpdateReason(&node) && (node.IsDrainState() || node.IsRebootRequestedState() || node.IsRebootIssuedState()) {
			worker.stage = workerRestoringSlurm
		} else {
			// Rollout-ready does not assert global Slurm schedulability.
			worker.stage = workerReady
		}
	}
}

func (o *rolloutObservation) observeCachedWorkerCleanup(cleanup *workerCleanupState) {
	if o == nil || o.workerSlurmRead || cleanup.lastCheck.IsZero() {
		return
	}
	for name, worker := range o.workers {
		if worker.replacing || !worker.ready {
			continue
		}
		if _, pending := cleanup.pendingWorkers[name]; !pending {
			worker.stage, worker.cached = workerReady, true
		}
	}
}

func (o *rolloutObservation) observeWorkerDecision(name string, node *slurmapi.Node, decision workerUpdateDecision) {
	if o == nil {
		return
	}
	worker := o.workers[name]
	if worker == nil {
		return
	}
	cpus, knownCPUs := node.CPUAllocated()
	worker.hasJobs = (knownCPUs && cpus > 0) || (node.AllocMemoryMB != nil && *node.AllocMemoryMB > 0) || node.IsCompletingState()
	switch decision.action {
	case workerUpdateActionUndrain:
		worker.stage = workerRestoringSlurm
	case workerUpdateActionWait:
		worker.stage = workerBlocked
	case workerUpdateActionTrackInFlight:
		if node.IsRebootIssuedState() {
			worker.stage = workerStopping
		} else {
			worker.stage = worker.requestedStage()
		}
	}
}

func (worker *workerStageObservation) requestedStage() workerStage {
	if worker.hasJobs {
		return workerWaitingForJobs
	}
	return workerWaitingForSlurm
}

func (o *rolloutObservation) observeWorkerRebootRequest(names []string, failed bool) {
	if o == nil {
		return
	}
	for _, name := range names {
		if worker := o.workers[name]; worker != nil {
			worker.stage = worker.requestedStage()
			if failed {
				worker.stage = workerBlocked
			}
		}
	}
}

func (o *rolloutObservation) workerCounts(sts *kruisev1b1.StatefulSet, previousCount int, failed bool) [workerStageCount]int {
	var counts [workerStageCount]int
	if !o.workerPodsKnown {
		counts[workerUnknown] = max(previousCount, int(desiredReplicas(sts)))
		return counts
	}
	for _, worker := range o.workers {
		stage := worker.stage
		if failed && worker.cached {
			stage = workerUnknown
		}
		counts[stage]++
	}
	// Terminating pods remain present until a later owned-Pod list omits them.
	counts[workerWaitingForPod] = max(0, int(desiredReplicas(sts))-len(o.workers))
	return counts
}
