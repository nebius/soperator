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

// +kubebuilder:rbac:groups=apps.kruise.io,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=apps.kruise.io,resources=statefulsets/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=apps.kruise.io,resources=statefulsets/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;update;patch;delete

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

	labels := sts.GetLabels()
	if labels[consts.LabelSoperatorRollingUpdateEnabled] != consts.LabelSoperatorRollingUpdateValue {
		logger.Info("rolling update is disabled", "namespace", req.Namespace, "name", req.Name)
		return ctrl.Result{}, nil
	}

	clusterName, ok := labels[consts.LabelInstanceKey]
	if !ok || clusterName == "" {
		return ctrl.Result{}, fmt.Errorf("missing cluster name label %s on statefulset %s/%s", consts.LabelInstanceKey, sts.Namespace, sts.Name)
	}

	replicas := defaultSTSReplicasCount
	if sts.Spec.Replicas != nil {
		replicas = *sts.Spec.Replicas
	}

	if sts.Status.UpdatedReplicas == replicas {
		podList, err := r.getPodList(ctx, sts)
		if err != nil {
			return ctrl.Result{}, err
		}

		undrainedNodes, err := r.cleanupStaleRollingUpdateDrains(ctx, clusterName, sts, podList)
		if err != nil {
			return ctrl.Result{}, err
		}
		if undrainedNodes > 0 {
			return ctrl.Result{RequeueAfter: time.Second}, nil
		}

		logger.Info("statefulset is up to date", "namespace", req.Namespace, "name", req.Name)
		return ctrl.Result{}, nil
	}

	outdatedPodList, err := r.getOutdatedPodList(ctx, sts)
	if err != nil {
		return ctrl.Result{}, err
	}

	operationID := sts.Status.UpdateRevision
	if operationID == "" {
		return ctrl.Result{}, fmt.Errorf("missing update revision on statefulset %s/%s", sts.Namespace, sts.Name)
	}

	if err := r.processRollingUpdate(ctx, clusterName, operationID, sts, outdatedPodList); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

func (r *RollingUpdateReconciler) getOutdatedPodList(
	ctx context.Context,
	sts *kruisev1b1.StatefulSet,
) ([]corev1.Pod, error) {
	podList, err := r.getPodList(ctx, sts)
	if err != nil {
		return nil, err
	}

	var res []corev1.Pod

	for _, pod := range podList {
		// TODO: REVISION
		podControllerRevisionHash := pod.Labels["controller-revision-hash"]
		if podControllerRevisionHash == sts.Status.UpdateRevision {
			continue
		}

		res = append(res, pod)
	}

	return res, nil
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

	return podList.Items, nil
}

func (r *RollingUpdateReconciler) processRollingUpdate(
	ctx context.Context,
	clusterName string,
	operationID string,
	sts *kruisev1b1.StatefulSet,
	outdatedPods []corev1.Pod,
) error {
	logger := log.FromContext(ctx).WithName("rolling-update-reconciler")

	if len(outdatedPods) == 0 {
		logger.Info("no outdated pods found", "namespace", sts.Namespace, "name", sts.Name)
		return nil
	}

	sort.Slice(outdatedPods, func(i, j int) bool {
		return outdatedPods[i].Name < outdatedPods[j].Name
	})

	podsToStop := make([]corev1.Pod, 0, len(outdatedPods))
	deletedPods := 0
	for _, pod := range outdatedPods {
		if workerOperationPhase(&pod, operationID) != consts.LabelSoperatorWorkerOperationPhaseReady {
			podsToStop = append(podsToStop, pod)
			continue
		}

		if err := r.Delete(ctx, &pod); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("delete pod %s/%s with completed worker handoff: %w", pod.Namespace, pod.Name, err)
		}
		deletedPods++
	}
	if deletedPods > 0 {
		logger.Info("deleted outdated pods with completed worker handoffs", "count", deletedPods, "operationID", operationID)
		return nil
	}

	for _, pod := range podsToStop {
		if !containerCrashLoopBackOff(pod.Status.InitContainerStatuses, consts.ContainerNameWorkerInit) {
			continue
		}

		if err := r.Delete(ctx, &pod); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("delete outdated pod %s/%s with crash-looping worker init: %w", pod.Namespace, pod.Name, err)
		}
		logger.Info(
			"deleted outdated pod with crash-looping worker init",
			"namespace", pod.Namespace,
			"pod", pod.Name,
		)
		return nil
	}

	slurmClient, ok := r.slurmAPIClients.GetClient(types.NamespacedName{
		Namespace: sts.Namespace,
		Name:      clusterName,
	})
	if !ok {
		logger.Info("no slurm api client", "namespace", sts.Namespace, "clusterName", clusterName)
		return fmt.Errorf("no slurm api client for %s/%s", sts.Namespace, clusterName)
	}

	type rebootCandidate struct {
		pod       corev1.Pod
		slurmNode slurmapi.Node
	}

	candidates := make([]rebootCandidate, 0, len(podsToStop))
	var undrainedNodes []string
	inFlightReady := 0
	for _, pod := range podsToStop {
		slurmNode, err := slurmClient.GetNode(ctx, pod.Name)
		if err != nil {
			return err
		}

		slurmdCrashLooping := containerCrashLoopBackOff(
			pod.Status.ContainerStatuses,
			consts.ContainerNameSlurmd,
		)
		rebootInProgress := slurmNode.IsRebootIssuedState() || slurmNode.IsRebootRequestedState()
		operationPhase := workerOperationPhase(&pod, operationID)
		rebootHandoffInProgress := rebootInProgress &&
			operationPhase == consts.LabelSoperatorWorkerOperationPhaseStopping
		managedRebootInProgress := rebootInProgress && hasRollingUpdateReason(&slurmNode)
		if staleRollingUpdateDrain(&slurmNode) {
			if err := slurmClient.UndrainNode(ctx, slurmNode.Name); err != nil {
				return fmt.Errorf("undrain stale rolling update node %s: %w", slurmNode.Name, err)
			}
			undrainedNodes = append(undrainedNodes, slurmNode.Name)
			continue
		}

		// Supervisord can keep the Pod Ready while repeatedly restarting slurmd.
		// Slurm state is the source of truth for safely completing an in-flight handoff.
		if (slurmdCrashLooping || rebootHandoffInProgress || managedRebootInProgress) &&
			safeToDeleteOfflineSlurmNode(&slurmNode) {
			if err := r.Delete(ctx, &pod); client.IgnoreNotFound(err) != nil {
				return fmt.Errorf("delete safely offline outdated pod %s/%s: %w", pod.Namespace, pod.Name, err)
			}
			logger.Info(
				"deleted safely offline outdated pod with no allocations",
				"namespace", pod.Namespace,
				"pod", pod.Name,
				"slurmNode", slurmNode.Name,
				"slurmdCrashLooping", slurmdCrashLooping,
				"rebootHandoffInProgress", rebootHandoffInProgress,
				"managedRebootInProgress", managedRebootInProgress,
				"operationID", operationID,
				"operationPhase", operationPhase,
			)
			return nil
		}

		if slurmdCrashLooping {

			logger.Info(
				"waiting to replace outdated pod with crash-looping slurmd",
				"namespace", pod.Namespace,
				"pod", pod.Name,
				"slurmNode", slurmNode.Name,
				"reason", "node is not safely offline with zero known allocations",
			)
			continue
		}

		if rebootInProgress {
			if podReady(&pod) {
				inFlightReady++
			}
			continue
		}

		candidates = append(candidates, rebootCandidate{pod: pod, slurmNode: slurmNode})
	}
	if len(undrainedNodes) > 0 {
		logger.Info("undrained stale rolling update nodes before reboot", "nodes", undrainedNodes)
		return nil
	}

	budget := rebootBudget(sts)
	unavailable := unavailableReplicas(sts)
	availableSlots := budget - unavailable - inFlightReady
	if availableSlots <= 0 {
		logger.Info(
			"rolling update budget is exhausted",
			"budget", budget,
			"unavailable", unavailable,
			"inFlightReady", inFlightReady,
		)
		return nil
	}

	slurmNodesToReboot := make([]string, 0, availableSlots)
	for _, candidate := range candidates {
		if len(slurmNodesToReboot) >= availableSlots {
			break
		}

		pod := candidate.pod
		if workerOperationPhase(&pod, operationID) != consts.LabelSoperatorWorkerOperationPhaseStopping {
			patchBase := pod.DeepCopy()
			if pod.Labels == nil {
				pod.Labels = map[string]string{}
			}
			pod.Labels[consts.LabelSoperatorWorkerOperationID] = operationID
			pod.Labels[consts.LabelSoperatorWorkerOperationPhase] =
				consts.LabelSoperatorWorkerOperationPhaseStopping
			if err := r.Patch(
				ctx,
				&pod,
				client.StrategicMergeFrom(patchBase, client.MergeFromWithOptimisticLock{}),
			); err != nil {
				return fmt.Errorf("start worker operation %s on pod %s/%s: %w", operationID, pod.Namespace, pod.Name, err)
			}
		}

		slurmNodesToReboot = append(slurmNodesToReboot, candidate.slurmNode.Name)
	}

	if len(slurmNodesToReboot) == 0 {
		logger.Info("all outdated pods already have reboot requested", "namespace", sts.Namespace, "name", sts.Name)
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

	logger.Info("scheduled slurm reboot through rest api", "nodes", slurmNodesToReboot)

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
	if raw := sts.GetAnnotations()[consts.AnnotationSoperatorRollingUpdateMaxUnavailable]; raw != "" {
		maxUnavailable = intstr.Parse(raw)
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
		Named(RollingUpdateControllerName).
		WithOptions(controllerconfig.ControllerOptions(maxConcurrency, cacheSyncTimeout))

	return controllerBuilder.Complete(r)
}

func rollingUpdateEnabled(obj client.Object) bool {
	if obj == nil {
		return false
	}
	return obj.GetLabels()[consts.LabelSoperatorRollingUpdateEnabled] == consts.LabelSoperatorRollingUpdateValue
}
