/*
Copyright 2025 Nebius B.V.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package updatecontroller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	api "github.com/SlinkyProject/slurm-client/api/v0044"
	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/clock"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/controller/reconciler"
	"nebius.ai/slurm-operator/internal/controllerconfig"
	"nebius.ai/slurm-operator/internal/slurmapi"
)

const (
	RollingUpdateControllerName = "rollingupdate"
)

const (
	defaultSTSReplicasCount = int32(1)
	defaultRebootReason     = "soperator rolling update"
)

type workerUpdateAction int

const (
	workerUpdateActionScheduleReboot workerUpdateAction = iota
	workerUpdateActionWait
	workerUpdateActionTrackInFlight
	workerUpdateActionUndrain
	workerUpdateActionDeleteOfflinePod
	workerUpdateActionDeleteFailedInitPod
	workerUpdateActionWaitNodeReplacement
	workerUpdateActionDeleteReadyPod
	workerUpdateActionRecoverPod
	workerUpdateActionReleasePod
)

type workerUpdateDecision struct {
	action                 workerUpdateAction
	operationPhase         string
	slurmdCrashLooping     bool
	workerInitCrashLooping bool
}

type workerReplacement struct {
	pod             corev1.Pod
	operationID     string
	k8sNodeCordoned bool
	preserveDrain   bool
}

type RollingUpdateReconciler struct {
	*reconciler.Reconciler

	slurmAPIClients        *slurmapi.ClientSet
	requeueAfter           time.Duration
	idleSlurmAuditInterval time.Duration
	clock                  clock.PassiveClock
	workerCleanup          workerCleanupTracker
	metrics                *rolloutMetrics
}

func NewRollingUpdateReconciler(
	client client.Client, scheme *runtime.Scheme,
	recorder record.EventRecorder,
	slurmAPIClients *slurmapi.ClientSet,
	requeueAfter time.Duration,
	idleSlurmAuditInterval time.Duration,
) *RollingUpdateReconciler {
	r := reconciler.NewReconciler(client, scheme, recorder)
	return &RollingUpdateReconciler{
		Reconciler:             r,
		slurmAPIClients:        slurmAPIClients,
		requeueAfter:           requeueAfter,
		idleSlurmAuditInterval: idleSlurmAuditInterval,
		clock:                  clock.RealClock{},
		metrics:                defaultRolloutMetrics,
	}
}

// +kubebuilder:rbac:groups=apps.kruise.io,resources=statefulsets,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps.kruise.io,resources=containerrecreaterequests,verbs=get;list;watch;create
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch

// Reconcile advances the periodic rollout loop for one NodeSet's worker StatefulSet.
func (r *RollingUpdateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName("rolling-update-reconciler").
		WithValues("namespace", req.Namespace, "name", req.Name)
	result := ctrl.Result{RequeueAfter: r.requeueAfter}

	// Returning an error would replace the configured interval with retry backoff.
	sts := &kruisev1b1.StatefulSet{}
	if err := r.Get(ctx, req.NamespacedName, sts); err != nil {
		if apierrors.IsNotFound(err) {
			r.workerCleanup.forget(req.NamespacedName)
			r.metrics.forget(req.NamespacedName)
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Get worker StatefulSet")
		r.metrics.readFailed(req.NamespacedName)
		return result, nil
	}
	if !rollingUpdateEnabled(sts) || sts.DeletionTimestamp != nil {
		r.workerCleanup.forget(req.NamespacedName)
		r.metrics.forget(req.NamespacedName)
		return ctrl.Result{}, nil
	}
	observation := &rolloutObservation{}
	ctx = context.WithValue(ctx, rolloutObservationKey{}, observation)
	err := r.reconcileWorkerStatefulSet(ctx, sts)
	if err != nil {
		logger.Error(err, "Reconcile worker StatefulSet")
	}
	r.metrics.observe(sts, observation, r.clock.Now(), err != nil)
	return result, nil
}

func (r *RollingUpdateReconciler) reconcileWorkerStatefulSet(ctx context.Context, sts *kruisev1b1.StatefulSet) error {
	clusterName := sts.Labels[consts.LabelInstanceKey]
	if clusterName == "" {
		return fmt.Errorf("read cluster name label %s on statefulset %s/%s", consts.LabelInstanceKey, sts.Namespace, sts.Name)
	}

	podList, err := r.getPodList(ctx, sts)
	if err != nil {
		return err
	}
	observationFromContext(ctx).observeWorkerPods(podList)
	k8sNodeCordonStates, err := r.getK8sNodeCordonStates(ctx, podList)
	if err != nil {
		return err
	}
	replacements := planWorkerPodReplacements(sts, podList, k8sNodeCordonStates)
	observationFromContext(ctx).observePlan(sts, podList, replacements)
	// StatefulSet status can lag behind pod events, particularly just after eviction.
	readyReplicas := int32(0)
	for _, pod := range podList {
		if pod.DeletionTimestamp == nil && podReady(&pod) {
			readyReplicas++
		}
	}
	sts.Status.ReadyReplicas = min(sts.Status.ReadyReplicas, readyReplicas)
	return r.processWorkerReplacements(ctx, clusterName, sts, replacements, podList)
}

func (r *RollingUpdateReconciler) getPodList(
	ctx context.Context,
	sts *kruisev1b1.StatefulSet,
) ([]corev1.Pod, error) {
	selector, err := metav1.LabelSelectorAsSelector(sts.Spec.Selector)
	if err != nil {
		return nil, fmt.Errorf("failed to convert label selector: %w", err)
	}

	podList := &corev1.PodList{}
	if err := r.List(ctx, podList,
		client.InNamespace(sts.Namespace),
		client.MatchingLabelsSelector{Selector: selector},
	); err != nil {
		return nil, fmt.Errorf("failed to list pods: %w", err)
	}

	var pods []corev1.Pod
	for _, pod := range podList.Items {
		if metav1.IsControlledBy(&pod, sts) {
			pods = append(pods, pod)
		}
	}
	return pods, nil
}

func planWorkerPodReplacements(
	sts *kruisev1b1.StatefulSet, pods []corev1.Pod, k8sNodeCordonStates map[string]bool,
) []workerReplacement {
	var replacements []workerReplacement
	for _, pod := range pods {
		if !metav1.IsControlledBy(&pod, sts) {
			continue
		}
		k8sNodeCordoned := k8sNodeCordonStates[pod.Spec.NodeName]
		operationID := pod.Labels[consts.LabelSoperatorWorkerOperationID]
		phase := pod.Labels[consts.LabelSoperatorWorkerOperationPhase]
		hasActiveHandoff := operationID != "" &&
			(phase == consts.LabelSoperatorWorkerOperationPhaseStopping ||
				phase == consts.LabelSoperatorWorkerOperationPhaseAcknowledged ||
				phase == consts.LabelSoperatorWorkerOperationPhaseReady ||
				phase == consts.LabelSoperatorWorkerOperationPhaseRecovering)
		needsRevisionUpdate := sts.Status.UpdateRevision != "" && pod.Labels["controller-revision-hash"] != sts.Status.UpdateRevision
		if !k8sNodeCordoned && !needsRevisionUpdate && !hasActiveHandoff {
			continue
		}

		// The worker acknowledges the exact operation ID it received from Slurm.
		// Preserve it across cordon changes and newer StatefulSet revisions.
		if !hasActiveHandoff {
			operationID = sts.Status.UpdateRevision
			if k8sNodeCordoned {
				operationID = "node-rollout-" + string(pod.UID)
			}
		}
		replacements = append(replacements, workerReplacement{
			pod:             pod,
			operationID:     operationID,
			k8sNodeCordoned: k8sNodeCordoned,
		})
	}
	return replacements
}

func (r *RollingUpdateReconciler) processWorkerReplacements(
	ctx context.Context,
	clusterName string,
	sts *kruisev1b1.StatefulSet,
	replacements []workerReplacement,
	pods []corev1.Pod,
) error {
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithName("rolling-update-reconciler").
		WithValues("namespace", sts.Namespace, "name", sts.Name))
	cleanup := r.workerCleanup.forStatefulSet(sts)
	cleanup.observePods(pods)
	observation := observationFromContext(ctx)
	if observation != nil {
		defer func() { observation.observeCleanup(cleanup) }()
	}
	cleanupPods := workerPodsWithoutReplacements(pods, replacements)

	prioritizeCordonedWorkers(replacements)
	var pending []workerReplacement
	for _, replacement := range replacements {
		if replacement.pod.DeletionTimestamp == nil {
			pending = append(pending, replacement)
		}
	}
	if observation != nil {
		observation.slots = max(0, rebootBudget(sts)-unavailableReplicas(sts))
	}
	if len(pending) == 0 &&
		(len(cleanupPods) == 0 || !cleanup.needsCheck(r.clock.Now(), r.idleSlurmAuditInterval)) {
		return nil
	}

	slurmClient, ok := r.slurmAPIClients.GetClient(types.NamespacedName{Namespace: sts.Namespace, Name: clusterName})
	if !ok {
		observation.waiting(waitSlurm)
		return fmt.Errorf("no slurm api client for %s/%s", sts.Namespace, clusterName)
	}
	slurmNodes, err := slurmClient.ListNodes(ctx)
	if err != nil {
		observation.waiting(waitSlurm)
		return err
	}
	// Observe before UNDRAIN or reboot: the next read must confirm their effects.
	cleanup.observeSlurmNodes(slurmNodes, r.clock.Now())
	observation.observeWorkerSlurm(slurmNodes)
	if len(replacements) > 0 {
		cleanup.trackReplacements(replacements)
	}
	nodesToUndrain := staleWorkerCleanupDrains(sts, cleanupPods, slurmNodes)
	candidates, readyPodsConsumingBudget, err := r.reconcileSlurmWorkerHandoffs(
		ctx, slurmClient, slurmNodes, pending, nodesToUndrain,
	)
	if err != nil || len(pending) == 0 {
		return err
	}
	availableSlots := availableWorkerHandoffSlots(ctx, sts, readyPodsConsumingBudget)
	if observation != nil {
		observation.slots = availableSlots
	}
	return r.startWorkerHandoffsWithinBudget(ctx, slurmClient, candidates, availableSlots)
}

func prioritizeCordonedWorkers(replacements []workerReplacement) {
	sort.Slice(replacements, func(i, j int) bool {
		if replacements[i].k8sNodeCordoned != replacements[j].k8sNodeCordoned {
			return replacements[i].k8sNodeCordoned
		}
		return replacements[i].pod.Name < replacements[j].pod.Name
	})
}

func (r *RollingUpdateReconciler) reconcileSlurmWorkerHandoffs(
	ctx context.Context, slurmClient slurmapi.Client, slurmNodes []slurmapi.Node,
	replacements []workerReplacement, nodesToUndrain []string,
) ([]workerReplacement, int, error) {
	logger := log.FromContext(ctx)
	slurmNodesByName := slurmNodesForReplacements(slurmNodes, replacements)

	var candidates []workerReplacement
	var missingSlurmNodes []string
	readyPodsConsumingBudget := 0
	for _, replacement := range replacements {
		pod := replacement.pod
		slurmNode, found := slurmNodesByName[pod.Name]
		decision := decideWorkerUpdateAction(replacement, &slurmNode)
		if !found {
			// Failed init can be replaced before first registration, but an existing
			// Slurm node must pass the health gate before we move its worker.
			if !replacement.k8sNodeCordoned &&
				decision.operationPhase != consts.LabelSoperatorWorkerOperationPhaseRecovering &&
				decision.workerInitCrashLooping {
				decision.action = workerUpdateActionDeleteFailedInitPod
			} else {
				observationFromContext(ctx).waiting(waitMissing)
				observationFromContext(ctx).setWorkerStage(pod.Name, workerMissingSlurmNode)
				missingSlurmNodes = append(missingSlurmNodes, pod.Name)
				// Unknown Slurm state consumes a slot even when Kubernetes reports Ready.
				if podReady(&pod) {
					readyPodsConsumingBudget++
				}
				continue
			}
		}

		if observation := observationFromContext(ctx); observation != nil {
			if slurmNode.IsRebootIssuedState() {
				observation.snapshot.Issued++
			} else if slurmNode.IsRebootRequestedState() {
				observation.snapshot.Requested++
			}
		}
		if workerOperationPhase(&pod, replacement.operationID) == consts.LabelSoperatorWorkerOperationPhaseRecovering {
			complete, err := r.reconcileWorkerRecovery(ctx, &pod, &slurmNode)
			if err != nil {
				observationFromContext(ctx).setWorkerStage(pod.Name, workerBlocked)
				return nil, 0, err
			}
			if !complete {
				observationFromContext(ctx).waiting(waitNodeReplacement)
				observationFromContext(ctx).setWorkerStage(pod.Name, workerWaitingForNodeReplacement)
				if podReady(&pod) {
					readyPodsConsumingBudget++
				}
				continue
			}
		}

		if decision.operationPhase == consts.LabelSoperatorWorkerOperationPhaseReady &&
			(decision.action == workerUpdateActionWaitNodeReplacement || decision.action == workerUpdateActionRecoverPod) {
			// Revoke the eviction release before waiting or attempting recovery.
			patchBase := pod.DeepCopy()
			pod.Labels[consts.LabelSoperatorWorkerOperationPhase] = consts.LabelSoperatorWorkerOperationPhaseAcknowledged
			if decision.action == workerUpdateActionRecoverPod {
				pod.Labels[consts.LabelSoperatorWorkerOperationPhase] = consts.LabelSoperatorWorkerOperationPhaseRecovering
			}
			if err := r.Patch(ctx, &pod, client.StrategicMergeFrom(patchBase, client.MergeFromWithOptimisticLock{})); err != nil {
				observationFromContext(ctx).setWorkerStage(pod.Name, workerBlocked)
				return nil, 0, fmt.Errorf("protect worker %s/%s for maintenance: %w", pod.Namespace, pod.Name, err)
			}
		}

		observationFromContext(ctx).observeWorkerDecision(pod.Name, &slurmNode, decision)

		switch decision.action {
		case workerUpdateActionWaitNodeReplacement:
			observationFromContext(ctx).waiting(waitNodeReplacement)
			logger.Info("Waiting for unhealthy worker's Kubernetes node to recover before rolling update",
				"pod", pod.Name, "slurmNode", slurmNode.Name, "reason", slurmNode.Reason)
			if podReady(&pod) && (slurmNode.IsRebootRequestedState() || slurmNode.IsRebootIssuedState() ||
				decision.operationPhase == consts.LabelSoperatorWorkerOperationPhaseRecovering ||
				decision.operationPhase == consts.LabelSoperatorWorkerOperationPhaseReady ||
				decision.operationPhase == consts.LabelSoperatorWorkerOperationPhaseAcknowledged) {
				readyPodsConsumingBudget++
			}
			continue
		case workerUpdateActionRecoverPod:
			observationFromContext(ctx).waiting(waitNodeReplacement)
			if _, err := r.reconcileWorkerRecovery(ctx, &pod, &slurmNode); err != nil {
				observationFromContext(ctx).setWorkerStage(pod.Name, workerBlocked)
				return nil, 0, err
			}
		case workerUpdateActionReleasePod:
			if err := r.releaseWorkerForEviction(ctx, &pod); err != nil {
				return nil, 0, err
			}
			observationFromContext(ctx).waiting(waitEviction)
			observationFromContext(ctx).setWorkerStage(pod.Name, workerWaitingForEviction)
		case workerUpdateActionDeleteReadyPod:
			if err := r.deleteWorkerPod(ctx, &pod); err != nil {
				return nil, 0, err
			}
		case workerUpdateActionUndrain:
			observationFromContext(ctx).waiting(waitCleanup)
			nodesToUndrain = append(nodesToUndrain, slurmNode.Name)
		case workerUpdateActionDeleteFailedInitPod:
			if err := r.deleteWorkerPod(ctx, &pod); err != nil {
				return nil, 0, err
			}
			logger.Info("Replacing worker with crash-looping init", "pod", pod.Name)
		case workerUpdateActionDeleteOfflinePod:
			if err := r.deleteWorkerPod(ctx, &pod); err != nil {
				return nil, 0, err
			}
			logger.Info(
				"Completed update of safely offline worker with no allocations",
				"pod", pod.Name,
				"slurmNode", slurmNode.Name,
				"slurmdCrashLooping", decision.slurmdCrashLooping,
				"operationID", replacement.operationID,
				"operationPhase", decision.operationPhase,
			)
		case workerUpdateActionWait:
			observationFromContext(ctx).waiting(waitSafety)
			logger.Info(
				"Waiting to replace worker with crash-looping slurmd",
				"pod", pod.Name,
				"slurmNode", slurmNode.Name,
				"reason", "node is not safely offline with zero known allocations",
			)
			continue
		case workerUpdateActionTrackInFlight:
			if slurmNode.IsRebootIssuedState() {
				observationFromContext(ctx).waiting(waitHandoff)
			} else {
				observationFromContext(ctx).waiting(waitReboot)
			}
		case workerUpdateActionScheduleReboot:
			replacement.preserveDrain = slurmNode.IsDrainState()
			candidates = append(candidates, replacement)
			continue
		}
		if podReady(&pod) {
			readyPodsConsumingBudget++
		}
	}
	if len(missingSlurmNodes) > 0 {
		logger.Info("Waiting for workers missing from Slurm node list", "nodes", missingSlurmNodes)
	}
	undrainStaleRollingUpdateNodes(ctx, slurmClient, nodesToUndrain)
	return candidates, readyPodsConsumingBudget, nil
}

func slurmNodesForReplacements(
	slurmNodes []slurmapi.Node, replacements []workerReplacement,
) map[string]slurmapi.Node {
	podNames := make(map[string]struct{}, len(replacements))
	for _, replacement := range replacements {
		podNames[replacement.pod.Name] = struct{}{}
	}
	nodesByName := make(map[string]slurmapi.Node, len(replacements))
	for _, node := range slurmNodes {
		if _, found := podNames[node.Name]; found {
			nodesByName[node.Name] = node
		}
	}
	return nodesByName
}

func availableWorkerHandoffSlots(ctx context.Context, sts *kruisev1b1.StatefulSet, readyPodsConsumingBudget int) int {
	budget := rebootBudget(sts)
	unavailable := unavailableReplicas(sts)
	availableSlots := max(0, budget-unavailable-readyPodsConsumingBudget)
	if availableSlots == 0 {
		log.FromContext(ctx).Info(
			"Rolling update budget is exhausted",
			"budget", budget,
			"unavailable", unavailable,
			"readyPodsConsumingBudget", readyPodsConsumingBudget,
		)
	}
	return availableSlots
}

func (r *RollingUpdateReconciler) startWorkerHandoffsWithinBudget(
	ctx context.Context, slurmClient slurmapi.Client, candidates []workerReplacement, availableSlots int,
) error {
	logger := log.FromContext(ctx)
	var slurmNodesToReboot []string
	var drainedSlurmNodesToReboot []string
	var handoffErrors []error
	for _, candidate := range candidates {
		pod := candidate.pod
		// Unready workers on draining nodes already consume the unavailable budget.
		// Their handoff must remain possible even when that budget is exhausted.
		if podReady(&pod) || !candidate.k8sNodeCordoned {
			if availableSlots == 0 {
				observationFromContext(ctx).waiting(waitBudget)
				observationFromContext(ctx).setWorkerStage(pod.Name, workerWaitingForSlot)
				continue
			}
			availableSlots--
		}
		if err := r.markWorkerOperationStopping(ctx, &pod, candidate.operationID); err != nil {
			observationFromContext(ctx).setWorkerStage(pod.Name, workerBlocked)
			// Keep the reserved slot: the pod may have become unavailable since the snapshot.
			handoffErrors = append(handoffErrors, err)
			continue
		}
		if candidate.preserveDrain {
			drainedSlurmNodesToReboot = append(drainedSlurmNodesToReboot, pod.Name)
		} else {
			slurmNodesToReboot = append(slurmNodesToReboot, pod.Name)
		}
	}
	if observation := observationFromContext(ctx); observation != nil {
		observation.slots = availableSlots
	}
	if len(slurmNodesToReboot) == 0 && len(drainedSlurmNodesToReboot) == 0 {
		logger.Info("No additional worker handoffs can be scheduled")
		return errors.Join(handoffErrors...)
	}

	// Drain explicitly and use plain reboot to avoid arming an automatic UNDRAIN.
	// Leave Reason unset on the reboot itself so health failures arriving after
	// the drain remain associated with the original Kubernetes node.
	var batches = []struct {
		nodes []string
		drain bool
	}{
		{nodes: slurmNodesToReboot, drain: true},
		{nodes: drainedSlurmNodesToReboot},
	}
	for _, batch := range batches {
		if len(batch.nodes) == 0 {
			continue
		}
		if batch.drain {
			if err := drainWorkersForReboot(ctx, slurmClient, batch.nodes); err != nil {
				observationFromContext(ctx).waiting(waitSlurm)
				observationFromContext(ctx).observeWorkerRebootRequest(batch.nodes, true)
				handoffErrors = append(handoffErrors, err)
				continue
			}
		}
		if err := slurmClient.RebootNodes(ctx, slurmapi.RebootNodesRequest{
			NodeList:    strings.Join(batch.nodes, ","),
			PowerAction: consts.SlurmPowerActionWorkerHandoff,
		}); err != nil {
			observationFromContext(ctx).waiting(waitSlurm)
			observationFromContext(ctx).observeWorkerRebootRequest(batch.nodes, true)
			handoffErrors = append(handoffErrors, fmt.Errorf("schedule slurm reboot through rest api: %w", err))
			continue
		}
		logger.Info("Scheduled Slurm reboot through REST API", "nodes", batch.nodes)
		observationFromContext(ctx).observeWorkerRebootRequest(batch.nodes, false)
		observationFromContext(ctx).waiting(waitReboot)
	}
	return errors.Join(handoffErrors...)
}

func drainWorkersForReboot(ctx context.Context, slurmClient slurmapi.Client, names []string) error {
	states := []api.V0044UpdateNodeMsgState{api.V0044UpdateNodeMsgStateDRAIN}
	reason := defaultRebootReason
	response, err := slurmClient.SlurmV0044PostNodesWithResponse(ctx, api.V0044UpdateNodeMsg{
		Name: &names, State: &states, Reason: &reason,
	})
	if err != nil {
		return fmt.Errorf("drain workers before reboot: %w", err)
	}
	if response == nil {
		return fmt.Errorf("drain workers before reboot: empty slurm response")
	}
	if response.StatusCode() < http.StatusOK || response.StatusCode() >= http.StatusMultipleChoices {
		return fmt.Errorf("drain workers before reboot: status=%d body=%s", response.StatusCode(), response.Body)
	}
	if response.JSON200 == nil {
		if len(response.Body) != 0 {
			return fmt.Errorf("drain workers before reboot: invalid slurm response: %s", response.Body)
		}
	} else if response.JSON200.Errors != nil && len(*response.JSON200.Errors) != 0 {
		return fmt.Errorf("drain workers before reboot: slurm errors: %v", *response.JSON200.Errors)
	}
	return nil
}

func (r *RollingUpdateReconciler) releaseWorkerForEviction(ctx context.Context, pod *corev1.Pod) error {
	if pod.Labels[consts.LabelSoperatorWorkerOperationPhase] == consts.LabelSoperatorWorkerOperationPhaseReady {
		return nil
	}
	patchBase := pod.DeepCopy()
	pod.Labels[consts.LabelSoperatorWorkerOperationPhase] = consts.LabelSoperatorWorkerOperationPhaseReady
	if err := r.Patch(ctx, pod, client.StrategicMergeFrom(patchBase, client.MergeFromWithOptimisticLock{})); err != nil {
		return fmt.Errorf("release worker %s/%s for eviction: %w", pod.Namespace, pod.Name, err)
	}
	return nil
}

func (r *RollingUpdateReconciler) markWorkerOperationStopping(ctx context.Context, pod *corev1.Pod, operationID string) error {
	if workerOperationPhase(pod, operationID) == consts.LabelSoperatorWorkerOperationPhaseStopping {
		return nil
	}
	patchBase := pod.DeepCopy()
	if pod.Labels == nil {
		pod.Labels = make(map[string]string)
	}
	pod.Labels[consts.LabelSoperatorWorkerOperationID] = operationID
	pod.Labels[consts.LabelSoperatorWorkerOperationPhase] = consts.LabelSoperatorWorkerOperationPhaseStopping
	delete(pod.Annotations, consts.AnnotationSoperatorWorkerRecoveryRequest)
	delete(pod.Annotations, consts.AnnotationSoperatorWorkerRecoveryContainerID)
	if err := r.Patch(ctx, pod, client.StrategicMergeFrom(patchBase, client.MergeFromWithOptimisticLock{})); err != nil {
		return fmt.Errorf("start worker operation %s on pod %s/%s: %w", operationID, pod.Namespace, pod.Name, err)
	}
	if observation := observationFromContext(ctx); observation != nil {
		observation.snapshot.Stopping++
	}
	return nil
}

func (r *RollingUpdateReconciler) deleteWorkerPod(ctx context.Context, pod *corev1.Pod) error {
	// Reject deletion when the pod or its operation changed since the observation.
	if err := r.Delete(ctx, pod, client.Preconditions{UID: &pod.UID, ResourceVersion: &pod.ResourceVersion}); client.IgnoreNotFound(err) != nil {
		observationFromContext(ctx).setWorkerStage(pod.Name, workerBlocked)
		return fmt.Errorf("delete worker pod %s/%s after handoff: %w", pod.Namespace, pod.Name, err)
	}
	observationFromContext(ctx).setWorkerStage(pod.Name, workerDeleting)
	observationFromContext(ctx).waiting(waitPods)
	return nil
}

func undrainStaleRollingUpdateNodes(ctx context.Context, slurmClient slurmapi.Client, nodeNames []string) {
	if len(nodeNames) == 0 {
		return
	}
	logger := log.FromContext(ctx)
	if err := slurmClient.UndrainNodes(ctx, nodeNames); err != nil {
		if observation := observationFromContext(ctx); observation != nil {
			observation.failed = true
			observation.waiting(waitSlurm)
			for _, name := range nodeNames {
				observation.setWorkerStage(name, workerBlocked)
			}
		}
		// A batch may apply partially. Re-select stale drains on the next pass
		// while allowing independent handoffs to proceed with their remaining budget.
		logger.Error(err, "Undrain stale rolling update nodes", "nodes", nodeNames)
		return
	}
	logger.Info("Undrained stale rolling update nodes", "nodes", nodeNames)
	observationFromContext(ctx).waiting(waitCleanup)
}

func rebootBudget(sts *kruisev1b1.StatefulSet) int {
	replicas := defaultSTSReplicasCount
	if sts.Spec.Replicas != nil {
		replicas = *sts.Spec.Replicas
	}
	if replicas <= 0 {
		return 0
	}

	maxUnavailable := intstr.FromInt32(1)
	if sts.Spec.ScaleStrategy != nil && sts.Spec.ScaleStrategy.MaxUnavailable != nil {
		maxUnavailable = *sts.Spec.ScaleStrategy.MaxUnavailable
	}

	budget, err := intstr.GetScaledValueFromIntOrPercent(&maxUnavailable, int(replicas), false)
	if err != nil || budget < 1 {
		return 1
	}
	if budget > int(replicas) {
		return int(replicas)
	}
	return budget
}

func unavailableReplicas(sts *kruisev1b1.StatefulSet) int {
	replicas := defaultSTSReplicasCount
	if sts.Spec.Replicas != nil {
		replicas = *sts.Spec.Replicas
	}
	unavailable := replicas - sts.Status.ReadyReplicas
	if unavailable < 0 {
		return 0
	}
	return int(unavailable)
}

func podReady(pod *corev1.Pod) bool {
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

func containerCrashLoopBackOff(statuses []corev1.ContainerStatus, containerName string) bool {
	for _, status := range statuses {
		if status.Name == containerName &&
			status.State.Waiting != nil &&
			status.State.Waiting.Reason == "CrashLoopBackOff" {
			return true
		}
	}
	return false
}

func workerOperationPhase(pod *corev1.Pod, operationID string) string {
	if pod.Labels[consts.LabelSoperatorWorkerOperationID] != operationID {
		return ""
	}
	return pod.Labels[consts.LabelSoperatorWorkerOperationPhase]
}

func decideWorkerUpdateAction(replacement workerReplacement, node *slurmapi.Node) workerUpdateDecision {
	pod := &replacement.pod
	rebootInProgress := node.IsRebootIssuedState() || node.IsRebootRequestedState()
	decision := workerUpdateDecision{
		operationPhase:         workerOperationPhase(pod, replacement.operationID),
		slurmdCrashLooping:     containerCrashLoopBackOff(pod.Status.ContainerStatuses, consts.ContainerNameSlurmd),
		workerInitCrashLooping: containerCrashLoopBackOff(pod.Status.InitContainerStatuses, consts.ContainerNameWorkerInit),
	}

	switch {
	case decision.operationPhase == consts.LabelSoperatorWorkerOperationPhaseRecovering &&
		(rebootInProgress || !workerRecoveryRegistered(node)):
		decision.action = workerUpdateActionWaitNodeReplacement
	case hasUnhealthyDrain(node) && node.IsRebootIssuedState() &&
		(decision.operationPhase == consts.LabelSoperatorWorkerOperationPhaseAcknowledged ||
			decision.operationPhase == consts.LabelSoperatorWorkerOperationPhaseReady):
		decision.action = workerUpdateActionRecoverPod
	case hasUnhealthyDrain(node):
		// Maintenance owns unhealthy workers, including those on cordoned hosts.
		decision.action = workerUpdateActionWaitNodeReplacement
	case replacement.k8sNodeCordoned &&
		(decision.operationPhase == consts.LabelSoperatorWorkerOperationPhaseAcknowledged ||
			decision.operationPhase == consts.LabelSoperatorWorkerOperationPhaseReady ||
			(decision.operationPhase == consts.LabelSoperatorWorkerOperationPhaseRecovering && node.IsDrainState())):
		decision.action = workerUpdateActionReleasePod
	case decision.operationPhase == consts.LabelSoperatorWorkerOperationPhaseReady ||
		decision.operationPhase == consts.LabelSoperatorWorkerOperationPhaseAcknowledged:
		decision.action = workerUpdateActionDeleteReadyPod
	case !replacement.k8sNodeCordoned && decision.workerInitCrashLooping:
		decision.action = workerUpdateActionDeleteFailedInitPod
	case decision.operationPhase != consts.LabelSoperatorWorkerOperationPhaseStopping &&
		decision.operationPhase != consts.LabelSoperatorWorkerOperationPhaseRecovering && staleRollingUpdateDrain(node):
		decision.action = workerUpdateActionUndrain
	case !replacement.k8sNodeCordoned && decision.slurmdCrashLooping && safeToDeleteOfflineSlurmNode(node):
		// An accepted reboot must reach the action script's acknowledgement.
		// Only a failed container can bypass that handshake after the health gate.
		decision.action = workerUpdateActionDeleteOfflinePod
	case !replacement.k8sNodeCordoned && decision.slurmdCrashLooping:
		decision.action = workerUpdateActionWait
	case rebootInProgress:
		decision.action = workerUpdateActionTrackInFlight
	default:
		decision.action = workerUpdateActionScheduleReboot
	}

	return decision
}

func workerRecoveryRegistered(node *slurmapi.Node) bool {
	cpus, known := node.CPUAllocated()
	return node.IsIdleState() && !node.IsCompletingState() &&
		!node.IsInvalidState() && !node.IsNotRespondingState() && known && cpus == 0 &&
		node.AllocMemoryMB != nil && *node.AllocMemoryMB == 0
}

func hasUnhealthyDrain(node *slurmapi.Node) bool {
	if !node.IsDrainState() || node.Reason == nil {
		return false
	}
	// Match the maintenance controller's first reason, including its precedence.
	// User problems do not request infrastructure recovery.
	for _, reason := range consts.SlurmNodeReasonsList {
		if strings.Contains(node.Reason.Reason, reason) {
			return reason != consts.SlurmUserReasonHC
		}
	}
	return false
}

func hasRollingUpdateReason(node *slurmapi.Node) bool {
	if node.Reason == nil {
		return false
	}
	reason := node.Reason.Reason
	return reason == defaultRebootReason || strings.HasPrefix(reason, defaultRebootReason+" : ")
}

func staleRollingUpdateDrain(node *slurmapi.Node) bool {
	if !node.IsDrainState() || !node.IsIdleState() || node.IsNotRespondingState() ||
		node.IsInvalidState() || node.IsCompletingState() {
		return false
	}
	if node.IsRebootIssuedState() || node.IsRebootRequestedState() {
		return false
	}
	return !hasUnhealthyDrain(node) && hasRollingUpdateReason(node)
}

// safeToDeleteOfflineSlurmNode identifies an offline worker with zero known allocations
// for immediate recovery deletion. This snapshot must not authorize deferred eviction.
func safeToDeleteOfflineSlurmNode(node *slurmapi.Node) bool {
	allocatedCPUs, cpusKnown := node.CPUAllocated()
	if !cpusKnown || allocatedCPUs != 0 {
		return false
	}
	if node.AllocMemoryMB == nil || *node.AllocMemoryMB != 0 {
		return false
	}
	if node.IsCompletingState() {
		return false
	}
	return node.IsDownState() || (node.IsIdleState() && node.IsNotRespondingState())
}

// SetupWithManager sets up the controller with the Manager.
func (r *RollingUpdateReconciler) SetupWithManager(
	mgr ctrl.Manager,
	maxConcurrency int,
	cacheSyncTimeout time.Duration,
) error {
	if r.requeueAfter <= 0 {
		return fmt.Errorf("configure a positive rolling update requeue interval, got %s", r.requeueAfter)
	}

	if r.idleSlurmAuditInterval <= 0 {
		return fmt.Errorf("configure a positive rolling update idle Slurm audit interval, got %s", r.idleSlurmAuditInterval)
	}

	// Keep these caches synchronized without enqueueing requests on resource events.
	for _, obj := range []client.Object{&corev1.Pod{}, &corev1.Node{}} {
		if _, err := mgr.GetCache().GetInformer(context.Background(), obj); err != nil {
			return fmt.Errorf("initialize rolling update cache for %T: %w", obj, err)
		}
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&kruisev1b1.StatefulSet{}, builder.WithPredicates(rollingUpdateLoopStartPredicate())).
		Named(RollingUpdateControllerName).
		WithOptions(controllerconfig.ControllerOptions(maxConcurrency, cacheSyncTimeout)).
		Complete(controllerconfig.NamedReconciler(RollingUpdateControllerName, r))
}

func rollingUpdateLoopStartPredicate() predicate.Funcs {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return rollingUpdateEnabled(e.Object)
		},
		// Start coordination when enabled; clean metric series when disabled.
		UpdateFunc: func(e event.UpdateEvent) bool {
			return rollingUpdateEnabled(e.ObjectOld) != rollingUpdateEnabled(e.ObjectNew)
		},
		DeleteFunc: func(e event.DeleteEvent) bool {
			return rollingUpdateEnabled(e.Object)
		},
		GenericFunc: func(event.GenericEvent) bool { return false },
	}
}

func rollingUpdateEnabled(obj client.Object) bool {
	sts, ok := obj.(*kruisev1b1.StatefulSet)
	if !ok || sts == nil {
		return false
	}
	return sts.Spec.UpdateStrategy.Type == appsv1.OnDeleteStatefulSetStrategyType &&
		sts.GetLabels()[consts.LabelWorkerKey] == consts.LabelWorkerValue
}
