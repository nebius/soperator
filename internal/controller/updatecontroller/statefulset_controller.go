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
	"sort"
	"strings"
	"time"

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
)

type workerUpdateDecision struct {
	action                  workerUpdateAction
	operationPhase          string
	slurmdCrashLooping      bool
	rebootHandoffInProgress bool
	managedRebootInProgress bool
}

type workerReplacement struct {
	pod             corev1.Pod
	operationID     string
	k8sNodeCordoned bool
}

type workerHandoffProgress struct {
	pending                  []workerReplacement
	readyPodsConsumingBudget int
}

type RollingUpdateReconciler struct {
	*reconciler.Reconciler

	slurmAPIClients        *slurmapi.ClientSet
	requeueAfter           time.Duration
	idleSlurmAuditInterval time.Duration
	clock                  clock.PassiveClock
	workerCleanup          workerCleanupTracker
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
	}
}

// +kubebuilder:rbac:groups=apps.kruise.io,resources=statefulsets,verbs=get;list;watch
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
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Get worker StatefulSet")
		return result, nil
	}
	if !rollingUpdateEnabled(sts) || sts.DeletionTimestamp != nil {
		r.workerCleanup.forget(req.NamespacedName)
		return ctrl.Result{}, nil
	}
	if err := r.reconcileWorkerStatefulSet(ctx, sts); err != nil {
		logger.Error(err, "Reconcile worker StatefulSet")
	}
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
	k8sNodeCordonStates, err := r.getK8sNodeCordonStates(ctx, podList)
	if err != nil {
		return err
	}
	replacements := planWorkerPodReplacements(sts, podList, k8sNodeCordonStates)
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
			(phase == consts.LabelSoperatorWorkerOperationPhaseStopping || phase == consts.LabelSoperatorWorkerOperationPhaseReady)
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
	cleanupPods := workerPodsWithoutReplacements(pods, replacements)

	prioritizeCordonedWorkers(replacements)
	progress, err := r.reconcileWorkerPodHandoffs(ctx, replacements)
	if err != nil {
		return err
	}
	if len(progress.pending) == 0 &&
		(len(cleanupPods) == 0 || !cleanup.needsCheck(r.clock.Now(), r.idleSlurmAuditInterval)) {
		return nil
	}

	slurmClient, ok := r.slurmAPIClients.GetClient(types.NamespacedName{Namespace: sts.Namespace, Name: clusterName})
	if !ok {
		return fmt.Errorf("no slurm api client for %s/%s", sts.Namespace, clusterName)
	}
	slurmNodes, err := slurmClient.ListNodes(ctx)
	if err != nil {
		return err
	}
	// Observe before UNDRAIN or reboot: the next read must confirm their effects.
	cleanup.observeSlurmNodes(slurmNodes, r.clock.Now())
	if len(replacements) > 0 {
		cleanup.trackReplacements(replacements)
	}
	nodesToUndrain := staleWorkerCleanupDrains(sts, cleanupPods, slurmNodes)
	candidates, readyPodsConsumingBudget, err := r.reconcileSlurmWorkerHandoffs(
		ctx, slurmClient, slurmNodes, progress.pending, nodesToUndrain,
	)
	if err != nil || len(progress.pending) == 0 {
		return err
	}
	readyPodsConsumingBudget += progress.readyPodsConsumingBudget
	availableSlots := availableWorkerHandoffSlots(ctx, sts, readyPodsConsumingBudget)
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

func (r *RollingUpdateReconciler) reconcileWorkerPodHandoffs(
	ctx context.Context, replacements []workerReplacement,
) (workerHandoffProgress, error) {
	var progress workerHandoffProgress
	for _, replacement := range replacements {
		pod := replacement.pod
		if pod.DeletionTimestamp != nil {
			continue
		}
		handoffReady := workerOperationPhase(&pod, replacement.operationID) == consts.LabelSoperatorWorkerOperationPhaseReady
		if replacement.k8sNodeCordoned && handoffReady {
			// A released worker still consumes capacity until its replacement is Ready.
			if podReady(&pod) {
				progress.readyPodsConsumingBudget++
			}
			continue
		}
		// A crash-loop snapshot cannot release a cordoned worker for later eviction:
		// it may recover and accept jobs before the drainer removes it.
		if handoffReady || (!replacement.k8sNodeCordoned &&
			containerCrashLoopBackOff(pod.Status.InitContainerStatuses, consts.ContainerNameWorkerInit)) {
			if err := r.deleteWorkerPod(ctx, &pod); err != nil {
				return workerHandoffProgress{}, err
			}
			// Account for this handoff against the readiness snapshot taken before deletion.
			// Unready pods are already included in unavailableReplicas.
			if podReady(&pod) {
				progress.readyPodsConsumingBudget++
			}
			continue
		}
		progress.pending = append(progress.pending, replacement)
	}
	return progress, nil
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
		if !found {
			missingSlurmNodes = append(missingSlurmNodes, pod.Name)
			// Unknown Slurm state consumes a slot even when Kubernetes reports Ready.
			if podReady(&pod) {
				readyPodsConsumingBudget++
			}
			continue
		}

		decision := decideWorkerUpdateAction(replacement, &slurmNode)
		switch decision.action {
		case workerUpdateActionUndrain:
			nodesToUndrain = append(nodesToUndrain, slurmNode.Name)
		case workerUpdateActionDeleteOfflinePod:
			if err := r.deleteWorkerPod(ctx, &pod); err != nil {
				return nil, 0, err
			}
			logger.Info(
				"Completed update of safely offline worker with no allocations",
				"pod", pod.Name,
				"slurmNode", slurmNode.Name,
				"slurmdCrashLooping", decision.slurmdCrashLooping,
				"rebootHandoffInProgress", decision.rebootHandoffInProgress,
				"managedRebootInProgress", decision.managedRebootInProgress,
				"operationID", replacement.operationID,
				"operationPhase", decision.operationPhase,
			)
		case workerUpdateActionWait:
			logger.Info(
				"Waiting to replace worker with crash-looping slurmd",
				"pod", pod.Name,
				"slurmNode", slurmNode.Name,
				"reason", "node is not safely offline with zero known allocations",
			)
			continue
		case workerUpdateActionTrackInFlight:
		case workerUpdateActionScheduleReboot:
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
	var handoffErrors []error
	for _, candidate := range candidates {
		pod := candidate.pod
		// Unready workers on draining nodes already consume the unavailable budget.
		// Their handoff must remain possible even when that budget is exhausted.
		if podReady(&pod) || !candidate.k8sNodeCordoned {
			if availableSlots == 0 {
				continue
			}
			availableSlots--
		}
		if err := r.markWorkerOperationStopping(ctx, &pod, candidate.operationID); err != nil {
			// Keep the reserved slot: the pod may have become unavailable since the snapshot.
			handoffErrors = append(handoffErrors, err)
			continue
		}
		slurmNodesToReboot = append(slurmNodesToReboot, pod.Name)
	}
	if len(slurmNodesToReboot) == 0 {
		logger.Info("No additional worker handoffs can be scheduled")
		return errors.Join(handoffErrors...)
	}

	if err := slurmClient.RebootNodes(ctx, slurmapi.RebootNodesRequest{
		NodeList:    strings.Join(slurmNodesToReboot, ","),
		ASAP:        true,
		Reason:      defaultRebootReason,
		PowerAction: consts.SlurmPowerActionWorkerHandoff,
	}); err != nil {
		handoffErrors = append(handoffErrors, fmt.Errorf("schedule slurm reboot through rest api: %w", err))
		return errors.Join(handoffErrors...)
	}
	logger.Info("Scheduled Slurm reboot through REST API", "nodes", slurmNodesToReboot)
	return errors.Join(handoffErrors...)
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
	if err := r.Patch(ctx, pod, client.StrategicMergeFrom(patchBase, client.MergeFromWithOptimisticLock{})); err != nil {
		return fmt.Errorf("start worker operation %s on pod %s/%s: %w", operationID, pod.Namespace, pod.Name, err)
	}
	return nil
}

func (r *RollingUpdateReconciler) deleteWorkerPod(ctx context.Context, pod *corev1.Pod) error {
	// UID preconditions prevent a delayed reconcile from deleting a replacement pod.
	if err := r.Delete(ctx, pod, client.Preconditions{UID: &pod.UID}); client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("delete worker pod %s/%s after handoff: %w", pod.Namespace, pod.Name, err)
	}
	return nil
}

func undrainStaleRollingUpdateNodes(ctx context.Context, slurmClient slurmapi.Client, nodeNames []string) {
	if len(nodeNames) == 0 {
		return
	}
	logger := log.FromContext(ctx)
	if err := slurmClient.UndrainNodes(ctx, nodeNames); err != nil {
		// A batch may apply partially. Re-select stale drains on the next pass
		// while allowing independent handoffs to proceed with their remaining budget.
		logger.Error(err, "Undrain stale rolling update nodes", "nodes", nodeNames)
		return
	}
	logger.Info("Undrained stale rolling update nodes", "nodes", nodeNames)
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
		operationPhase:          workerOperationPhase(pod, replacement.operationID),
		slurmdCrashLooping:      containerCrashLoopBackOff(pod.Status.ContainerStatuses, consts.ContainerNameSlurmd),
		managedRebootInProgress: rebootInProgress && hasRollingUpdateReason(node),
	}
	decision.rebootHandoffInProgress = rebootInProgress &&
		decision.operationPhase == consts.LabelSoperatorWorkerOperationPhaseStopping

	switch {
	case staleRollingUpdateDrain(node):
		decision.action = workerUpdateActionUndrain
	case !replacement.k8sNodeCordoned && (decision.slurmdCrashLooping ||
		decision.rebootHandoffInProgress ||
		decision.managedRebootInProgress) && safeToDeleteOfflineSlurmNode(node):
		// Supervisord can keep the Pod Ready while repeatedly restarting slurmd.
		// Slurm state is the source of truth for safely completing an in-flight handoff.
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
	return hasRollingUpdateReason(node)
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
		Complete(r)
}

func rollingUpdateLoopStartPredicate() predicate.Funcs {
	return predicate.Funcs{
		CreateFunc: func(e event.CreateEvent) bool {
			return rollingUpdateEnabled(e.Object)
		},
		// Start a loop when coordination is enabled on an existing NodeSet.
		UpdateFunc: func(e event.UpdateEvent) bool {
			return !rollingUpdateEnabled(e.ObjectOld) && rollingUpdateEnabled(e.ObjectNew)
		},
		DeleteFunc:  func(event.DeleteEvent) bool { return false },
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
