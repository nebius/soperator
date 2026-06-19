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
		logger.Info("statefulset is up to date", "namespace", req.Namespace, "name", req.Name)
		return ctrl.Result{}, nil
	}

	outdatedPodList, err := r.getOutdatedPodList(ctx, sts)
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := r.processRollingUpdate(ctx, clusterName, sts, outdatedPodList); err != nil {
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

func (r *RollingUpdateReconciler) getOutdatedPodList(
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

	var res []corev1.Pod

	for _, pod := range podList.Items {
		// TODO: REVISION
		podControllerRevisionHash := pod.Labels["controller-revision-hash"]
		if podControllerRevisionHash == sts.Status.UpdateRevision {
			continue
		}

		res = append(res, pod)
	}

	return res, nil
}

func (r *RollingUpdateReconciler) processRollingUpdate(
	ctx context.Context,
	clusterName string,
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
		if pod.Labels[consts.LabelSoperatorDeleteCandidate] != consts.LabelSoperatorDeleteCandidateValueDeleting {
			podsToStop = append(podsToStop, pod)
			continue
		}

		if err := r.Delete(ctx, &pod); client.IgnoreNotFound(err) != nil {
			return fmt.Errorf("delete pod %s/%s marked as delete candidate: %w", pod.Namespace, pod.Name, err)
		}
		deletedPods++
	}
	if deletedPods > 0 {
		logger.Info("deleted outdated pods marked as delete candidates", "count", deletedPods)
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
	inFlightReady := 0
	for _, pod := range podsToStop {
		slurmNode, err := slurmClient.GetNode(ctx, pod.Name)
		if err != nil {
			return err
		}

		if slurmNode.IsRebootIssuedState() || slurmNode.IsRebootRequestedState() {
			if podReady(&pod) {
				inFlightReady++
			}
			continue
		}

		candidates = append(candidates, rebootCandidate{pod: pod, slurmNode: slurmNode})
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
		if pod.Labels[consts.LabelSoperatorDeleteCandidate] != consts.LabelSoperatorDeleteCandidateValueStopping {
			patchBase := pod.DeepCopy()
			if pod.Labels == nil {
				pod.Labels = map[string]string{}
			}
			pod.Labels[consts.LabelSoperatorDeleteCandidate] = consts.LabelSoperatorDeleteCandidateValueStopping
			if err := r.Patch(ctx, &pod, client.MergeFrom(patchBase)); err != nil {
				return fmt.Errorf("label pod %s/%s for reboot handoff: %w", pod.Namespace, pod.Name, err)
			}
		}

		slurmNodesToReboot = append(slurmNodesToReboot, candidate.slurmNode.Name)
	}

	if len(slurmNodesToReboot) == 0 {
		logger.Info("all outdated pods already have reboot requested", "namespace", sts.Namespace, "name", sts.Name)
		return nil
	}

	if err := slurmClient.RebootNodes(ctx, slurmapi.RebootNodesRequest{
		NodeList: strings.Join(slurmNodesToReboot, ","),
		Reason:   defaultRebootReason,
	}); err != nil {
		return fmt.Errorf("schedule slurm reboot through rest api: %w", err)
	}

	logger.Info("scheduled slurm reboot through rest api", "nodes", slurmNodesToReboot)

	return nil
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
