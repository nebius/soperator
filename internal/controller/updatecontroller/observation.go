package updatecontroller

import (
	"context"
	"crypto/sha256"
	"sort"
	"strings"
	"time"

	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"

	"nebius.ai/slurm-operator/internal/consts"
)

// Each object retains aggregate counts and one hash, never a list of workers.
type rolloutSnapshot struct {
	Incarnations                                   [sha256.Size]byte
	Total, Outdated, Updated, Ready, Replacing     int
	Stopping, Released, Requested, Issued, Cleanup int
}

type rolloutState struct {
	Revision           string
	Desired            int32
	Active             bool
	LastProgress       int64
	Snapshot, Progress rolloutSnapshot
}

type rolloutObservationKey struct{}

// Operation helpers share only the observation belonging to this reconciliation.
type rolloutObservation struct {
	planKnown, cleanupKnown, failed  bool
	snapshot                         rolloutSnapshot
	slots                            int
	wait                             string
	workerPodsKnown, workerSlurmRead bool
	workers                          map[string]*workerStageObservation
}

func observationFromContext(ctx context.Context) *rolloutObservation {
	observation, _ := ctx.Value(rolloutObservationKey{}).(*rolloutObservation)
	return observation
}

func (o *rolloutObservation) waiting(reason string) {
	if o == nil {
		return
	}
	// Errors and unsafe worker state outrank expected waits.
	priority := map[string]int{waitPods: 1, waitCleanup: 2, waitReboot: 3, waitHandoff: 4, waitBudget: 5, waitEviction: 6, waitSafety: 7, waitMissing: 8, waitSlurm: 9, waitError: 10}
	if priority[reason] > priority[o.wait] {
		o.wait = reason
	}
}

func (o *rolloutObservation) observePlan(sts *kruisev1b1.StatefulSet, pods []corev1.Pod, replacements []workerReplacement) {
	if o == nil {
		return
	}
	o.planKnown = true
	o.observeWorkerPlan(sts, pods, replacements)
	o.snapshot = rolloutSnapshot{Total: len(pods), Replacing: len(replacements)}
	var incarnations []string
	for _, pod := range pods {
		incarnations = append(incarnations, pod.Name+"/"+string(pod.UID))
		revision := pod.Labels["controller-revision-hash"]
		if sts.Status.UpdateRevision != "" && revision != sts.Status.UpdateRevision {
			o.snapshot.Outdated++
		} else if sts.Status.UpdateRevision != "" && pod.DeletionTimestamp == nil {
			o.snapshot.Updated++
			if podReady(&pod) {
				o.snapshot.Ready++
			}
		}
		if pod.Labels[consts.LabelSoperatorWorkerOperationID] != "" {
			switch pod.Labels[consts.LabelSoperatorWorkerOperationPhase] {
			case consts.LabelSoperatorWorkerOperationPhaseStopping:
				o.snapshot.Stopping++
			case consts.LabelSoperatorWorkerOperationPhaseReady:
				o.snapshot.Released++
			}
		}
	}
	sort.Strings(incarnations)
	o.snapshot.Incarnations = sha256.Sum256([]byte(strings.Join(incarnations, "\n")))
}

func (o *rolloutObservation) observeCleanup(cleanup *workerCleanupState) {
	if o == nil {
		return
	}
	o.cleanupKnown = !cleanup.lastCheck.IsZero()
	o.observeCachedWorkerCleanup(cleanup)
	o.snapshot.Cleanup = len(cleanup.pendingWorkers)
	if o.snapshot.Cleanup > 0 {
		o.waiting(waitCleanup)
	}
}

func desiredReplicas(sts *kruisev1b1.StatefulSet) int32 {
	if sts.Spec.Replicas != nil {
		return *sts.Spec.Replicas
	}
	return defaultSTSReplicasCount
}

func rolloutReady(sts *kruisev1b1.StatefulSet, s rolloutSnapshot) bool {
	desired := int(desiredReplicas(sts))
	if desired == 0 && s.Total == 0 && s.Replacing == 0 && s.Cleanup == 0 {
		return true
	}
	return sts.Status.ObservedGeneration >= sts.Generation && sts.Status.UpdateRevision != "" &&
		s.Outdated == 0 && s.Updated == desired && s.Ready == desired && s.Total == desired &&
		s.Replacing == 0 && s.Cleanup == 0
}

func (s *rolloutState) observeProgress(snapshot rolloutSnapshot) bool {
	old := s.Progress
	// High-water marks ignore readiness flaps and repeated requests. Incarnation
	// changes count real replacements even when a cordon window stays the same size.
	advanced := snapshot.Outdated < old.Outdated || snapshot.Replacing < old.Replacing || snapshot.Updated > old.Updated || snapshot.Ready > old.Ready ||
		snapshot.Stopping > old.Stopping || snapshot.Released > old.Released || snapshot.Requested > old.Requested || snapshot.Issued > old.Issued || snapshot.Cleanup < old.Cleanup ||
		(s.Snapshot.Incarnations != [sha256.Size]byte{} && snapshot.Incarnations != s.Snapshot.Incarnations)
	s.Progress = rolloutSnapshot{
		Outdated: min(old.Outdated, snapshot.Outdated), Replacing: min(old.Replacing, snapshot.Replacing), Updated: max(old.Updated, snapshot.Updated), Ready: max(old.Ready, snapshot.Ready),
		Stopping: max(old.Stopping, snapshot.Stopping), Released: max(old.Released, snapshot.Released), Requested: max(old.Requested, snapshot.Requested), Issued: max(old.Issued, snapshot.Issued), Cleanup: min(old.Cleanup, snapshot.Cleanup),
	}
	s.Snapshot = snapshot
	return advanced
}

func (o *rolloutObservation) advance(sts *kruisev1b1.StatefulSet, state rolloutState, now time.Time, reconcileFailed bool) rolloutState {
	failed := reconcileFailed || o.failed
	if o.planKnown {
		if !o.cleanupKnown && (failed || o.snapshot.Total > 0) {
			o.snapshot.Cleanup = state.Snapshot.Cleanup
		}
		ready := rolloutReady(sts, o.snapshot)
		if !ready && (!state.Active || state.Revision != sts.Status.UpdateRevision || state.Desired != desiredReplicas(sts)) {
			state = rolloutState{Revision: sts.Status.UpdateRevision, Desired: desiredReplicas(sts), Active: true, LastProgress: now.Unix(), Progress: o.snapshot}
		}
		if ready && !failed {
			if state.Active {
				state.LastProgress = now.Unix()
			}
			state.Active, state.Revision, state.Desired = false, sts.Status.UpdateRevision, desiredReplicas(sts)
			o.wait = ""
		}
		if state.observeProgress(o.snapshot) && state.Active {
			state.LastProgress = now.Unix()
		}
	}
	if failed {
		o.slots = 0
		if o.wait != waitSlurm {
			o.waiting(waitError)
		}
	} else if state.Active && o.wait == "" {
		o.wait = waitPods
	}
	return state
}
