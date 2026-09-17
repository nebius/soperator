package updatecontroller

import (
	"sync"
	"time"

	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/slurmapi"
)

type workerCleanupTracker struct {
	mu     sync.Mutex
	states map[types.NamespacedName]*workerCleanupState
}

type workerCleanupState struct {
	statefulSetUID types.UID
	clusterName    string
	observedPods   map[string]workerCleanupPod
	pendingWorkers map[string]struct{}
	lastCheck      time.Time
	checkRequired  bool
}

type workerCleanupPod struct {
	uid         types.UID
	ready       bool
	terminating bool
}

func (t *workerCleanupTracker) forStatefulSet(sts *kruisev1b1.StatefulSet) *workerCleanupState {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.states == nil {
		t.states = make(map[types.NamespacedName]*workerCleanupState)
	}
	key := client.ObjectKeyFromObject(sts)
	state := t.states[key]
	clusterName := sts.Labels[consts.LabelInstanceKey]
	if state == nil || state.statefulSetUID != sts.UID || state.clusterName != clusterName {
		state = &workerCleanupState{
			statefulSetUID: sts.UID,
			clusterName:    clusterName,
			pendingWorkers: make(map[string]struct{}),
		}
		t.states[key] = state
	}
	// Controller-runtime serializes reconciliations for a key. Other NodeSets
	// need only the map lock and must not wait for this NodeSet's Slurm requests.
	return state
}

func (t *workerCleanupTracker) forget(key types.NamespacedName) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.states, key)
}

func (s *workerCleanupState) observePods(pods []corev1.Pod) {
	observed := make(map[string]workerCleanupPod, len(pods))
	for _, pod := range pods {
		current := workerCleanupPod{uid: pod.UID, ready: podReady(&pod), terminating: pod.DeletionTimestamp != nil}
		previous, found := s.observedPods[pod.Name]
		if !found || previous != current {
			s.checkRequired = true
		}
		observed[pod.Name] = current
	}
	if len(observed) != len(s.observedPods) {
		s.checkRequired = true
	}
	// A missing pod may have been scaled down or be between incarnations. Its
	// return always triggers a fresh check, even if it is already Ready.
	for name := range s.pendingWorkers {
		if _, exists := observed[name]; !exists {
			delete(s.pendingWorkers, name)
		}
	}
	s.observedPods = observed
}

func (s *workerCleanupState) trackReplacements(replacements []workerReplacement) {
	s.checkRequired = true
	for _, replacement := range replacements {
		s.pendingWorkers[replacement.pod.Name] = struct{}{}
	}
}

func (s *workerCleanupState) needsCheck(now time.Time, idleSlurmAuditInterval time.Duration) bool {
	return s.checkRequired || len(s.pendingWorkers) > 0 || s.lastCheck.IsZero() ||
		now.Sub(s.lastCheck) >= idleSlurmAuditInterval
}

func (s *workerCleanupState) observeSlurmNodes(nodes []slurmapi.Node, now time.Time) {
	pending := make(map[string]struct{}, len(s.observedPods))
	for name := range s.observedPods {
		// Missing Slurm nodes leave cleanup unresolved until a later successful observation.
		pending[name] = struct{}{}
	}
	for _, node := range nodes {
		if hasRollingUpdateReason(&node) &&
			(node.IsDrainState() || node.IsRebootRequestedState() || node.IsRebootIssuedState()) {
			continue
		}
		delete(pending, node.Name)
	}
	s.pendingWorkers = pending
	s.lastCheck = now
	s.checkRequired = false
}

func workerPodsWithoutReplacements(pods []corev1.Pod, replacements []workerReplacement) []corev1.Pod {
	if len(replacements) == 0 {
		return pods
	}
	replacing := make(map[string]struct{}, len(replacements))
	for _, replacement := range replacements {
		replacing[replacement.pod.Name] = struct{}{}
	}
	var cleanupPods []corev1.Pod
	for _, pod := range pods {
		if _, found := replacing[pod.Name]; !found {
			cleanupPods = append(cleanupPods, pod)
		}
	}
	return cleanupPods
}

func staleWorkerCleanupDrains(
	sts *kruisev1b1.StatefulSet, pods []corev1.Pod, slurmNodes []slurmapi.Node,
) []string {
	eligibleNodeNames := make(map[string]struct{}, len(pods))
	for _, pod := range pods {
		if pod.DeletionTimestamp == nil &&
			pod.Labels["controller-revision-hash"] == sts.Status.UpdateRevision && podReady(&pod) {
			eligibleNodeNames[pod.Name] = struct{}{}
		}
	}
	var nodesToUndrain []string
	for _, slurmNode := range slurmNodes {
		if _, ok := eligibleNodeNames[slurmNode.Name]; !ok {
			continue
		}
		if staleRollingUpdateDrain(&slurmNode) {
			nodesToUndrain = append(nodesToUndrain, slurmNode.Name)
		}
	}
	return nodesToUndrain
}
