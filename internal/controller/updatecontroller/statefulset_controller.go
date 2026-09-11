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
	"fmt"
	"sort"
	"strings"
	"time"

	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
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
	workerUpdateActionCompleteHandoff
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
	completedHandoffs        int
}

type RollingUpdateReconciler struct {
	*reconciler.Reconciler

	slurmAPIClients *slurmapi.ClientSet
}

func NewRollingUpdateReconciler(
	client client.Client, scheme *runtime.Scheme,
	recorder record.EventRecorder,
	slurmAPIClients *slurmapi.ClientSet,
) *RollingUpdateReconciler {
	r := reconciler.NewReconciler(client, scheme, recorder)
	return &RollingUpdateReconciler{
		Reconciler:      r,
		slurmAPIClients: slurmAPIClients,
	}
}

// +kubebuilder:rbac:groups=apps.kruise.io,resources=statefulsets,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.0/pkg/reconcile
func (r *RollingUpdateReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName("rolling-update-reconciler")
	logger.Info("reconciling statefulset", "namespace", req.Namespace, "name", req.Name)

	sts := &kruisev1b1.StatefulSet{}
	err := r.Get(ctx, req.NamespacedName, sts)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			logger.Info("statefulset not found, might be deleted", "namespace", req.Namespace, "name", req.Name)
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if !rollingUpdateEnabled(sts) {
		logger.Info("rolling update is disabled", "namespace", req.Namespace, "name", req.Name)
		return ctrl.Result{}, nil
	}

	labels := sts.GetLabels()
	clusterName, ok := labels[consts.LabelInstanceKey]
	if !ok || clusterName == "" {
		return ctrl.Result{}, fmt.Errorf("missing cluster name label %s on statefulset %s/%s", consts.LabelInstanceKey, sts.Namespace, sts.Name)
	}

	podList, err := r.getPodList(ctx, sts)
	if err != nil {
		return ctrl.Result{}, err
	}
	k8sNodeCordonStates, err := r.getK8sNodeCordonStates(ctx, podList)
	if err != nil {
		return ctrl.Result{}, err
	}
	replacements := planWorkerPodReplacements(sts, podList, k8sNodeCordonStates)
	if len(replacements) == 0 {
		undrainedNodes, err := r.cleanupStaleRollingUpdateDrains(ctx, clusterName, sts, podList)
		if err != nil {
			return ctrl.Result{}, err
		}
		if undrainedNodes > 0 {
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}
		return ctrl.Result{}, nil
	}
	// StatefulSet status can lag behind pod events, particularly just after eviction.
	readyReplicas := int32(0)
	for _, pod := range podList {
		if pod.DeletionTimestamp == nil && podReady(&pod) {
			readyReplicas++
		}
	}
	sts.Status.ReadyReplicas = min(sts.Status.ReadyReplicas, readyReplicas)
	if err := r.processWorkerReplacements(ctx, clusterName, sts, replacements); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: time.Minute}, nil
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
) error {
	if len(replacements) == 0 {
		return nil
	}
	ctx = log.IntoContext(ctx, log.FromContext(ctx).WithName("rolling-update-reconciler").
		WithValues("namespace", sts.Namespace, "name", sts.Name))

	prioritizeCordonedWorkers(replacements)
	progress, err := r.reconcileWorkerPodHandoffs(ctx, replacements)
	if err != nil {
		return err
	}
	if progress.completedHandoffs > 0 || len(progress.pending) == 0 {
		return nil
	}

	slurmClient, ok := r.slurmAPIClients.GetClient(types.NamespacedName{Namespace: sts.Namespace, Name: clusterName})
	if !ok {
		return fmt.Errorf("no slurm api client for %s/%s", sts.Namespace, clusterName)
	}
	candidates, readyPodsConsumingBudget, err := r.reconcileSlurmWorkerHandoffs(ctx, slurmClient, progress.pending)
	if err != nil {
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
		if handoffReady || containerCrashLoopBackOff(pod.Status.InitContainerStatuses, consts.ContainerNameWorkerInit) {
			if err := r.finishWorkerHandoff(ctx, replacement); err != nil {
				return workerHandoffProgress{}, err
			}
			progress.completedHandoffs++
			continue
		}
		progress.pending = append(progress.pending, replacement)
	}
	return progress, nil
}

func (r *RollingUpdateReconciler) reconcileSlurmWorkerHandoffs(
	ctx context.Context, slurmClient slurmapi.Client, replacements []workerReplacement,
) ([]workerReplacement, int, error) {
	logger := log.FromContext(ctx)
	slurmNodesByName, err := getSlurmNodesForReplacements(ctx, slurmClient, replacements)
	if err != nil {
		return nil, 0, err
	}

	var candidates []workerReplacement
	var undrainedNodes []string
	readyPodsConsumingBudget := 0
	for _, replacement := range replacements {
		pod := replacement.pod
		slurmNode, found := slurmNodesByName[pod.Name]
		if !found {
			return nil, 0, fmt.Errorf("slurm node %s is missing from list nodes response", pod.Name)
		}

		decision := decideWorkerUpdateAction(&pod, &slurmNode, replacement.operationID)
		switch decision.action {
		case workerUpdateActionUndrain:
			if err := slurmClient.UndrainNode(ctx, slurmNode.Name); err != nil {
				return nil, 0, fmt.Errorf("undrain stale rolling update node %s: %w", slurmNode.Name, err)
			}
			undrainedNodes = append(undrainedNodes, slurmNode.Name)
		case workerUpdateActionCompleteHandoff:
			if err := r.finishWorkerHandoff(ctx, replacement); err != nil {
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
	if len(undrainedNodes) > 0 {
		logger.Info("Undrained stale rolling update nodes before reboot", "nodes", undrainedNodes)
	}
	return candidates, readyPodsConsumingBudget, nil
}

func getSlurmNodesForReplacements(
	ctx context.Context, slurmClient slurmapi.Client, replacements []workerReplacement,
) (map[string]slurmapi.Node, error) {
	slurmNodes, err := slurmClient.ListNodes(ctx)
	if err != nil {
		return nil, err
	}
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
	return nodesByName, nil
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
			return err
		}
		slurmNodesToReboot = append(slurmNodesToReboot, pod.Name)
	}
	if len(slurmNodesToReboot) == 0 {
		logger.Info("No additional worker handoffs can be scheduled")
		return nil
	}

	if err := slurmClient.RebootNodes(ctx, slurmapi.RebootNodesRequest{
		NodeList:    strings.Join(slurmNodesToReboot, ","),
		ASAP:        true,
		Reason:      defaultRebootReason,
		PowerAction: consts.SlurmPowerActionWorkerHandoff,
	}); err != nil {
		return fmt.Errorf("schedule slurm reboot through rest api: %w", err)
	}
	logger.Info("Scheduled Slurm reboot through REST API", "nodes", slurmNodesToReboot)
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
	if err := r.Patch(ctx, pod, client.StrategicMergeFrom(patchBase, client.MergeFromWithOptimisticLock{})); err != nil {
		return fmt.Errorf("start worker operation %s on pod %s/%s: %w", operationID, pod.Namespace, pod.Name, err)
	}
	return nil
}

func (r *RollingUpdateReconciler) finishWorkerHandoff(ctx context.Context, replacement workerReplacement) error {
	if replacement.k8sNodeCordoned {
		return r.markWorkerOperationReady(ctx, &replacement.pod, replacement.operationID)
	}
	return r.deleteWorkerPod(ctx, &replacement.pod)
}

func (r *RollingUpdateReconciler) deleteWorkerPod(ctx context.Context, pod *corev1.Pod) error {
	// UID preconditions prevent a delayed reconcile from deleting a replacement pod.
	if err := r.Delete(ctx, pod, client.Preconditions{UID: &pod.UID}); client.IgnoreNotFound(err) != nil {
		return fmt.Errorf("delete worker pod %s/%s after handoff: %w", pod.Namespace, pod.Name, err)
	}
	return nil
}

func (r *RollingUpdateReconciler) markWorkerOperationReady(ctx context.Context, pod *corev1.Pod, operationID string) error {
	if operationID == "" {
		return fmt.Errorf("mark worker pod %s/%s ready: operation ID is empty", pod.Namespace, pod.Name)
	}
	base := pod.DeepCopy()
	if pod.Labels == nil {
		pod.Labels = make(map[string]string)
	}
	pod.Labels[consts.LabelSoperatorWorkerOperationID] = operationID
	pod.Labels[consts.LabelSoperatorWorkerOperationPhase] = consts.LabelSoperatorWorkerOperationPhaseReady
	if err := r.Patch(ctx, pod, client.StrategicMergeFrom(base, client.MergeFromWithOptimisticLock{})); err != nil {
		return fmt.Errorf("mark worker operation ready on pod %s/%s: %w", pod.Namespace, pod.Name, err)
	}
	log.FromContext(ctx).Info("Marked worker operation ready for eviction", "pod", pod.Name, "node", pod.Spec.NodeName, "operationID", operationID)
	return nil
}

func (r *RollingUpdateReconciler) cleanupStaleRollingUpdateDrains(
	ctx context.Context,
	clusterName string,
	sts *kruisev1b1.StatefulSet,
	pods []corev1.Pod,
) (int, error) {
	logger := log.FromContext(ctx).WithName("rolling-update-reconciler")
	eligibleNodeNames := make(map[string]struct{}, len(pods))
	for _, pod := range pods {
		if pod.Labels["controller-revision-hash"] == sts.Status.UpdateRevision && podReady(&pod) {
			eligibleNodeNames[pod.Name] = struct{}{}
		}
	}
	if len(eligibleNodeNames) == 0 {
		return 0, nil
	}

	slurmClient, ok := r.slurmAPIClients.GetClient(types.NamespacedName{
		Namespace: sts.Namespace,
		Name:      clusterName,
	})
	if !ok {
		return 0, fmt.Errorf("no slurm api client for %s/%s", sts.Namespace, clusterName)
	}
	slurmNodes, err := slurmClient.ListNodes(ctx)
	if err != nil {
		return 0, err
	}

	var undrainedNodes []string
	for _, slurmNode := range slurmNodes {
		if _, ok := eligibleNodeNames[slurmNode.Name]; !ok {
			continue
		}
		if !staleRollingUpdateDrain(&slurmNode) {
			continue
		}
		if err := slurmClient.UndrainNode(ctx, slurmNode.Name); err != nil {
			return 0, fmt.Errorf("undrain stale rolling update node %s: %w", slurmNode.Name, err)
		}
		undrainedNodes = append(undrainedNodes, slurmNode.Name)
	}

	if len(undrainedNodes) > 0 {
		logger.Info("undrained stale rolling update nodes after update", "nodes", undrainedNodes)
	}
	return len(undrainedNodes), nil
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

func decideWorkerUpdateAction(
	pod *corev1.Pod,
	node *slurmapi.Node,
	operationID string,
) workerUpdateDecision {
	rebootInProgress := node.IsRebootIssuedState() || node.IsRebootRequestedState()
	decision := workerUpdateDecision{
		operationPhase:          workerOperationPhase(pod, operationID),
		slurmdCrashLooping:      containerCrashLoopBackOff(pod.Status.ContainerStatuses, consts.ContainerNameSlurmd),
		managedRebootInProgress: rebootInProgress && hasRollingUpdateReason(node),
	}
	decision.rebootHandoffInProgress = rebootInProgress &&
		decision.operationPhase == consts.LabelSoperatorWorkerOperationPhaseStopping

	switch {
	case staleRollingUpdateDrain(node):
		decision.action = workerUpdateActionUndrain
	case (decision.slurmdCrashLooping ||
		decision.rebootHandoffInProgress ||
		decision.managedRebootInProgress) && safeToDeleteOfflineSlurmNode(node):
		// Supervisord can keep the Pod Ready while repeatedly restarting slurmd.
		// Slurm state is the source of truth for safely completing an in-flight handoff.
		decision.action = workerUpdateActionCompleteHandoff
	case decision.slurmdCrashLooping:
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

// safeToDeleteOfflineSlurmNode requires both zero known allocations and
// an offline Slurm state, so deleting the Pod cannot race with new scheduling.
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
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &corev1.Pod{}, workerPodNodeNameIndex,
		func(obj client.Object) []string {
			pod := obj.(*corev1.Pod)
			if pod.Spec.NodeName == "" {
				return nil
			}
			return []string{pod.Spec.NodeName}
		}); err != nil {
		return fmt.Errorf("index worker nodes: %w", err)
	}

	controllerBuilder := ctrl.NewControllerManagedBy(mgr).
		For(&kruisev1b1.StatefulSet{}, builder.WithPredicates(predicate.Funcs{
			CreateFunc: func(tce event.TypedCreateEvent[client.Object]) bool {
				return rollingUpdateEnabled(tce.Object)
			},
			UpdateFunc: func(tue event.TypedUpdateEvent[client.Object]) bool {
				return rollingUpdateEnabled(tue.ObjectNew)
			},
			DeleteFunc:  func(tde event.TypedDeleteEvent[client.Object]) bool { return false },
			GenericFunc: func(tge event.TypedGenericEvent[client.Object]) bool { return false },
		})).
		Owns(&corev1.Pod{}).
		Watches(&corev1.Node{}, handler.EnqueueRequestsFromMapFunc(r.mapNodeToStatefulSetRequests),
			builder.WithPredicates(predicate.Funcs{
				CreateFunc: func(e event.CreateEvent) bool {
					return e.Object.(*corev1.Node).Spec.Unschedulable
				},
				UpdateFunc: func(e event.UpdateEvent) bool {
					return e.ObjectOld.(*corev1.Node).Spec.Unschedulable != e.ObjectNew.(*corev1.Node).Spec.Unschedulable
				},
			})).
		Named(RollingUpdateControllerName).
		WithOptions(controllerconfig.ControllerOptions(maxConcurrency, cacheSyncTimeout))

	return controllerBuilder.Complete(r)
}

func rollingUpdateEnabled(obj client.Object) bool {
	sts, ok := obj.(*kruisev1b1.StatefulSet)
	if !ok || sts == nil {
		return false
	}
	return sts.Spec.UpdateStrategy.Type == appsv1.OnDeleteStatefulSetStrategyType &&
		sts.GetLabels()[consts.LabelWorkerKey] == consts.LabelWorkerValue
}
