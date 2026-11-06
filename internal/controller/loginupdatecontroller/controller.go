package loginupdatecontroller

import (
	"context"
	"fmt"
	"slices"
	"sort"
	"strings"
	"time"

	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"
	"github.com/robfig/cron/v3"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
	"k8s.io/utils/clock"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/controllerconfig"
	"nebius.ai/slurm-operator/internal/values"
)

const (
	ControllerName      = "loginupdate"
	DefaultRequeueAfter = time.Minute
)

// Reconciler schedules replacement of outdated login pods and accepts requests
// for an early replacement of a specific pod instance.
type Reconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	Recorder     events.EventRecorder
	clock        clock.PassiveClock
	metrics      *rolloutMetrics
	requeueAfter time.Duration
}

func NewReconciler(c client.Client, scheme *runtime.Scheme, recorder events.EventRecorder, requeueAfter time.Duration) *Reconciler {
	return &Reconciler{
		Client: c, Scheme: scheme, Recorder: recorder,
		clock: clock.RealClock{}, metrics: defaultRolloutMetrics, requeueAfter: requeueAfter,
	}
}

// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=slurmclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=apps.kruise.io,resources=statefulsets,verbs=get;list;watch;patch
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;patch;delete

func (r *Reconciler) Reconcile(ctx context.Context, req ctrl.Request) (result ctrl.Result, reconcileErr error) {
	logger := log.FromContext(ctx).WithName("login-update-reconciler").
		WithValues("namespace", req.Namespace, "name", req.Name)
	ctx = log.IntoContext(ctx, logger)
	logger.V(1).Info("Reconciling login pod updates")

	sts := &kruisev1b1.StatefulSet{}
	if err := r.Get(ctx, req.NamespacedName, sts); err != nil {
		if apierrors.IsNotFound(err) {
			r.metrics.forget(req.NamespacedName)
		} else {
			r.metrics.readFailed(req.NamespacedName)
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !enabled(sts) || sts.DeletionTimestamp != nil {
		r.metrics.forget(req.NamespacedName)
		logger.V(1).Info("Skipping ineligible login StatefulSet", "updateStrategy", sts.Spec.UpdateStrategy.Type, "deleting", sts.DeletionTimestamp != nil)
		return ctrl.Result{}, nil
	}
	cluster := &slurmv1.SlurmCluster{}
	owner := metav1.GetControllerOf(sts)
	if err := r.Get(ctx, client.ObjectKey{Namespace: sts.Namespace, Name: owner.Name}, cluster); err != nil {
		if apierrors.IsNotFound(err) {
			r.metrics.forget(req.NamespacedName)
		} else {
			r.metrics.readFailed(req.NamespacedName)
		}
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if !clusterEligible(sts, cluster) {
		r.metrics.forget(req.NamespacedName)
		logger.V(1).Info("Skipping login updates because the owning SlurmCluster is ineligible")
		return ctrl.Result{}, nil
	}
	logger = logger.WithValues("slurmCluster", cluster.Name, "updateRevision", sts.Status.UpdateRevision)
	ctx = log.IntoContext(ctx, logger)
	rollingUpdate := cluster.Spec.SlurmNodes.Login.UpdateStrategy == consts.UpdateStrategyRollingUpdate
	if rollingUpdate || sts.Spec.UpdateStrategy.Type != appsv1.OnDeleteStatefulSetStrategyType {
		r.metrics.forget(req.NamespacedName)
	}
	if !rollingUpdate && sts.Spec.UpdateStrategy.Type != appsv1.OnDeleteStatefulSetStrategyType {
		return ctrl.Result{}, nil
	}
	pods, err := r.pods(ctx, sts)
	if err != nil {
		r.metrics.readFailed(req.NamespacedName)
		return ctrl.Result{}, err
	}
	if rollingUpdate {
		for i := range pods {
			if err := r.clearSchedule(ctx, &pods[i]); err != nil {
				return ctrl.Result{}, err
			}
		}
		if sts.Annotations[consts.AnnotationLoginUpdateInProgress] != "" {
			return ctrl.Result{}, r.setActiveReplacement(ctx, sts, nil)
		}
		return ctrl.Result{}, nil
	}
	waitReason := ""
	defer func() {
		r.metrics.observe(sts, pods, r.clock.Now(), waitReason, reconcileErr != nil)
	}()
	minNotice, schedule, err := values.ResolveLoginUpdatePolicy(cluster.Spec.SlurmNodes.Login.UpdatePolicy)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("resolve login update policy for SlurmCluster %s/%s: %w", cluster.Namespace, cluster.Name, err)
	}
	if sts.Status.ObservedGeneration < sts.Generation || sts.Status.UpdateRevision == "" {
		waitReason = waitTargetRevision
		logger.V(1).Info("Waiting for the login StatefulSet to observe its target revision", "generation", sts.Generation, "observedGeneration", sts.Status.ObservedGeneration)
		return ctrl.Result{RequeueAfter: r.requeueAfter}, nil
	}

	updatePlan, err := r.reconcilePodSchedules(ctx, sts, pods, minNotice, schedule)
	if err != nil {
		return ctrl.Result{}, err
	}
	replacementInProgress, err := r.reconcileActiveReplacement(ctx, sts, pods, updatePlan.restartCandidates)
	if err != nil {
		return ctrl.Result{}, err
	}
	if replacementInProgress {
		waitReason = waitPods
		return ctrl.Result{RequeueAfter: r.requeueAfter}, nil
	}
	if len(updatePlan.restartCandidates) == 0 {
		if updatePlan.waitingForReplacement {
			waitReason = waitPods
			return ctrl.Result{RequeueAfter: r.requeueAfter}, nil
		}
		if updatePlan.nextScheduledAt.IsZero() {
			return ctrl.Result{}, nil
		}
		waitReason = waitNotice
		// A deadline that passed during reconciliation still needs a positive timer.
		return ctrl.Result{RequeueAfter: max(updatePlan.nextScheduledAt.Sub(r.clock.Now()), time.Nanosecond)}, nil
	}
	waitReason = waitPods
	if err := r.replaceFirstEligiblePod(ctx, sts, pods, updatePlan.restartCandidates); err != nil {
		return ctrl.Result{}, err
	}
	return ctrl.Result{RequeueAfter: r.requeueAfter}, nil
}

type podUpdatePlan struct {
	restartCandidates     []corev1.Pod
	nextScheduledAt       time.Time
	waitingForReplacement bool
}

func (r *Reconciler) reconcilePodSchedules(ctx context.Context, sts *kruisev1b1.StatefulSet, pods []corev1.Pod, minNotice time.Duration, schedule cron.Schedule) (podUpdatePlan, error) {
	logger := log.FromContext(ctx)
	plan := podUpdatePlan{
		waitingForReplacement: len(pods) < int(ptr.Deref(sts.Spec.Replicas, 0)),
	}
	activePodName, originalPodUID, _ := strings.Cut(sts.Annotations[consts.AnnotationLoginUpdateInProgress], "/")
	for i := range pods {
		pod := &pods[i]
		if pod.DeletionTimestamp != nil {
			logger.V(1).Info("Skipping terminating login pod", "pod", pod.Name, "podUID", pod.UID)
			plan.waitingForReplacement = true
			continue
		}
		if pod.Labels[appsv1.StatefulSetRevisionLabel] == sts.Status.UpdateRevision {
			if err := r.clearSchedule(ctx, pod); err != nil {
				return podUpdatePlan{}, err
			}
			continue
		}
		if pod.Name == activePodName && string(pod.UID) != originalPodUID {
			continue
		}
		scheduledAt, err := r.ensureSchedule(ctx, pod, minNotice, schedule)
		if err != nil {
			return podUpdatePlan{}, err
		}
		earlyRestartRequested := pod.Annotations[consts.AnnotationLoginUpdateRequested] == string(pod.UID)
		ready := podReady(pod)
		if !ready || earlyRestartRequested || !r.clock.Now().Before(scheduledAt) {
			logger.V(1).Info("Login pod is eligible for replacement", "pod", pod.Name, "podUID", pod.UID, "ready", ready, "earlyRestartRequested", earlyRestartRequested, "scheduledAt", scheduledAt.Format(time.RFC3339Nano))
			plan.restartCandidates = append(plan.restartCandidates, *pod)
		} else {
			logger.V(1).Info("Waiting for the login pod replacement time", "pod", pod.Name, "scheduledAt", scheduledAt.Format(time.RFC3339Nano))
			if plan.nextScheduledAt.IsZero() || scheduledAt.Before(plan.nextScheduledAt) {
				plan.nextScheduledAt = scheduledAt
			}
		}
	}
	return plan, nil
}

func (r *Reconciler) replaceFirstEligiblePod(ctx context.Context, sts *kruisev1b1.StatefulSet, pods, candidates []corev1.Pod) error {
	if !allReadyOrBroken(ctx, sts, pods) {
		return nil
	}
	// Repair unavailable pods first, keeping name order within each group.
	sort.Slice(candidates, func(i, j int) bool {
		iReady, jReady := podReady(&candidates[i]), podReady(&candidates[j])
		if iReady != jReady {
			return !iReady
		}
		return candidates[i].Name < candidates[j].Name
	})
	if err := r.setActiveReplacement(ctx, sts, &candidates[0]); err != nil {
		return err
	}
	return r.replace(ctx, sts, &candidates[0])
}

func (r *Reconciler) reconcileActiveReplacement(ctx context.Context, sts *kruisev1b1.StatefulSet, pods, restartCandidates []corev1.Pod) (bool, error) {
	podName, originalPodUID, _ := strings.Cut(sts.Annotations[consts.AnnotationLoginUpdateInProgress], "/")
	if podName == "" {
		return false, nil
	}
	logger := log.FromContext(ctx).WithValues("activePod", podName)
	for i := range pods {
		pod := &pods[i]
		if pod.Name != podName {
			continue
		}
		if pod.DeletionTimestamp != nil {
			logger.V(1).Info("Waiting for the active login pod to terminate")
			return true, nil
		}
		if string(pod.UID) == originalPodUID && !slices.ContainsFunc(restartCandidates, func(candidate corev1.Pod) bool {
			return candidate.UID == pod.UID
		}) {
			logger.V(1).Info("Clearing active replacement because the original login pod is no longer eligible for restart")
			return false, r.setActiveReplacement(ctx, sts, nil)
		}
		if pod.Labels[appsv1.StatefulSetRevisionLabel] != sts.Status.UpdateRevision {
			if !allReadyOrBroken(ctx, sts, pods) {
				return true, nil
			}
			logger.Info("Continuing the active login pod replacement with the latest revision")
			return true, r.replace(ctx, sts, pod)
		}
		if !podReady(pod) {
			logger.V(1).Info("Waiting for the active login pod replacement to become ready", "phase", pod.Status.Phase)
			return true, nil
		}
		logger.Info("Completed the active login pod replacement")
		return false, r.setActiveReplacement(ctx, sts, nil)
	}
	for ordinal := range ptr.Deref(sts.Spec.Replicas, 0) {
		if podName == fmt.Sprintf("%s-%d", sts.Name, ordinal) {
			logger.V(1).Info("Waiting for the active login pod replacement to be created")
			return true, nil
		}
	}
	logger.Info("Clearing the active login pod replacement after scale-down")
	return false, r.setActiveReplacement(ctx, sts, nil)
}

func (r *Reconciler) setActiveReplacement(ctx context.Context, sts *kruisev1b1.StatefulSet, pod *corev1.Pod) error {
	base := sts.DeepCopy()
	if pod == nil {
		delete(sts.Annotations, consts.AnnotationLoginUpdateInProgress)
	} else {
		if sts.Annotations == nil {
			sts.Annotations = make(map[string]string)
		}
		sts.Annotations[consts.AnnotationLoginUpdateInProgress] = pod.Name + "/" + string(pod.UID)
	}
	if err := r.Patch(ctx, sts, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
		return fmt.Errorf("persist active login pod replacement: %w", err)
	}
	return nil
}

func enabled(sts *kruisev1b1.StatefulSet) bool {
	if sts.Labels[consts.LabelNameKey] != consts.LabelNameValue ||
		sts.Labels[consts.LabelManagedByKey] != consts.LabelManagedByValue ||
		sts.Labels[consts.LabelInstanceKey] == "" ||
		sts.Labels[consts.LabelComponentKey] != consts.ComponentTypeLogin.String() {
		return false
	}
	owner := metav1.GetControllerOf(sts)
	return owner != nil && owner.UID != "" && owner.Name != "" && owner.Kind == slurmv1.KindSlurmCluster && owner.APIVersion == slurmv1.GroupVersion.String()
}

func clusterEligible(sts *kruisev1b1.StatefulSet, cluster *slurmv1.SlurmCluster) bool {
	owner := metav1.GetControllerOf(sts)
	return owner != nil && cluster.UID == owner.UID && cluster.Name == owner.Name &&
		cluster.Name == sts.Labels[consts.LabelInstanceKey] && cluster.DeletionTimestamp == nil
}

func (r *Reconciler) pods(ctx context.Context, sts *kruisev1b1.StatefulSet) ([]corev1.Pod, error) {
	if sts.Spec.Selector == nil || sts.UID == "" {
		return nil, fmt.Errorf("read selector and identity of login statefulset %s/%s", sts.Namespace, sts.Name)
	}
	selector, err := metav1.LabelSelectorAsSelector(sts.Spec.Selector)
	if err != nil {
		return nil, fmt.Errorf("parse login pod selector: %w", err)
	}
	list := &corev1.PodList{}
	if err := r.List(ctx, list, client.InNamespace(sts.Namespace), client.MatchingLabelsSelector{Selector: selector}); err != nil {
		return nil, fmt.Errorf("list login pods: %w", err)
	}
	var pods []corev1.Pod
	for _, pod := range list.Items {
		if metav1.IsControlledBy(&pod, sts) {
			pods = append(pods, pod)
		}
	}
	return pods, nil
}

func (r *Reconciler) ensureSchedule(ctx context.Context, pod *corev1.Pod, minNotice time.Duration, schedule cron.Schedule) (time.Time, error) {
	announced := pod.Annotations[consts.AnnotationLoginUpdateAnnouncedAt]
	scheduled := pod.Annotations[consts.AnnotationLoginUpdateScheduledAt]
	announcedAt := r.clock.Now().UTC()
	var previousScheduledAt time.Time
	if announced != "" || scheduled != "" {
		var announcedErr, scheduledErr error
		announcedAt, announcedErr = time.Parse(time.RFC3339Nano, announced)
		previousScheduledAt, scheduledErr = time.Parse(time.RFC3339Nano, scheduled)
		if announcedErr != nil || scheduledErr != nil || previousScheduledAt.Before(announcedAt) {
			return time.Time{}, fmt.Errorf("read persisted login update schedule on pod %s/%s", pod.Namespace, pod.Name)
		}
	}

	noticeEndsAt := announcedAt.UTC().Add(minNotice)
	scheduledAt := noticeEndsAt
	if schedule != nil {
		// Include a cron occurrence exactly at the end of the notice period.
		scheduledAt = schedule.Next(noticeEndsAt.Add(-time.Nanosecond)).UTC()
	}
	if scheduledAt.IsZero() || scheduledAt.Before(noticeEndsAt) {
		return time.Time{}, fmt.Errorf("find a login update window after the notice period")
	}
	if scheduledAt.Equal(previousScheduledAt) {
		return scheduledAt, nil
	}
	base := pod.DeepCopy()
	if pod.Annotations == nil {
		pod.Annotations = make(map[string]string)
	}
	if announced == "" {
		pod.Annotations[consts.AnnotationLoginUpdateAnnouncedAt] = announcedAt.Format(time.RFC3339Nano)
	}
	pod.Annotations[consts.AnnotationLoginUpdateScheduledAt] = scheduledAt.Format(time.RFC3339Nano)
	if err := r.Patch(ctx, pod, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
		return time.Time{}, fmt.Errorf("persist login update schedule: %w", err)
	}
	log.FromContext(ctx).Info("Scheduled login pod replacement", "pod", pod.Name, "podUID", pod.UID,
		"announcedAt", announcedAt.Format(time.RFC3339Nano), "scheduledAt", scheduledAt.Format(time.RFC3339Nano), "minNotice", minNotice.String())
	r.Recorder.Eventf(pod, nil, corev1.EventTypeNormal, "LoginUpdateScheduled", "ScheduleLoginUpdate", "Login pod will be replaced at or after %s; an administrator can request an earlier restart", scheduledAt.Format(time.RFC3339))
	return scheduledAt, nil
}

func (r *Reconciler) clearSchedule(ctx context.Context, pod *corev1.Pod) error {
	base := pod.DeepCopy()
	for _, annotation := range []string{consts.AnnotationLoginUpdateAnnouncedAt, consts.AnnotationLoginUpdateScheduledAt, consts.AnnotationLoginUpdateRequested} {
		delete(pod.Annotations, annotation)
	}
	if len(base.Annotations) == len(pod.Annotations) {
		return nil
	}
	if err := r.Patch(ctx, pod, client.MergeFromWithOptions(base, client.MergeFromWithOptimisticLock{})); err != nil {
		return fmt.Errorf("clear login update schedule: %w", err)
	}
	log.FromContext(ctx).Info("Cleared login update metadata", "pod", pod.Name, "podUID", pod.UID)
	return nil
}

func allReadyOrBroken(ctx context.Context, sts *kruisev1b1.StatefulSet, pods []corev1.Pod) bool {
	logger := log.FromContext(ctx)
	desiredReplicas := ptr.Deref(sts.Spec.Replicas, 0)
	if len(pods) != int(desiredReplicas) {
		logger.V(1).Info("Waiting for the desired number of login pods before replacement", "desiredReplicas", desiredReplicas, "observedPods", len(pods))
		return false
	}
	for _, pod := range pods {
		if pod.DeletionTimestamp != nil {
			logger.V(1).Info("Waiting for terminating login pod before replacement", "blockingPod", pod.Name)
			return false
		}
		if pod.Labels[appsv1.StatefulSetRevisionLabel] != sts.Status.UpdateRevision || podReady(&pod) {
			continue
		}
		logger.V(1).Info("Waiting for updated login pod to become ready before replacement", "blockingPod", pod.Name, "phase", pod.Status.Phase)
		return false
	}
	return true
}

func podReady(pod *corev1.Pod) bool {
	if pod.Status.Phase != corev1.PodRunning {
		return false
	}
	for _, condition := range pod.Status.Conditions {
		if condition.Type == corev1.PodReady && condition.Status == corev1.ConditionTrue {
			return true
		}
	}
	return false
}

func (r *Reconciler) replace(ctx context.Context, sts *kruisev1b1.StatefulSet, pod *corev1.Pod) error {
	uid, resourceVersion := pod.UID, pod.ResourceVersion
	if err := r.Delete(ctx, pod, client.Preconditions{UID: &uid, ResourceVersion: &resourceVersion}); err != nil {
		return fmt.Errorf("replace outdated login pod %s/%s: %w", pod.Namespace, pod.Name, err)
	}
	earlyRestartRequested := pod.Annotations[consts.AnnotationLoginUpdateRequested] == string(pod.UID)
	log.FromContext(ctx).Info("Requested replacement of outdated login pod", "pod", pod.Name, "podUID", pod.UID,
		"earlyRestartRequested", earlyRestartRequested, "scheduledAt", pod.Annotations[consts.AnnotationLoginUpdateScheduledAt],
		"podRevision", pod.Labels[appsv1.StatefulSetRevisionLabel])
	r.Recorder.Eventf(sts, pod, corev1.EventTypeNormal, "LoginPodRestarting", "ReplaceLoginPod", "Requested replacement of outdated login pod %s (early restart requested: %t)", pod.Name, earlyRestartRequested)
	return nil
}

func (r *Reconciler) clusterRequests(ctx context.Context, obj client.Object) []ctrl.Request {
	cluster, ok := obj.(*slurmv1.SlurmCluster)
	if !ok {
		return nil
	}
	list := &kruisev1b1.StatefulSetList{}
	if err := r.List(ctx, list, client.InNamespace(cluster.Namespace), client.MatchingLabels{
		consts.LabelInstanceKey: cluster.Name, consts.LabelComponentKey: consts.ComponentTypeLogin.String(),
	}); err != nil {
		log.FromContext(ctx).Error(err, "List login StatefulSets for a cluster policy change")
		return nil
	}
	var requests []ctrl.Request
	for i := range list.Items {
		sts := &list.Items[i]
		if enabled(sts) && metav1.IsControlledBy(sts, cluster) {
			requests = append(requests, ctrl.Request{NamespacedName: client.ObjectKeyFromObject(sts)})
		}
	}
	return requests
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager, maxConcurrency int, cacheSyncTimeout time.Duration) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&kruisev1b1.StatefulSet{}, builder.WithPredicates(predicate.NewPredicateFuncs(func(obj client.Object) bool {
			sts, ok := obj.(*kruisev1b1.StatefulSet)
			return ok && enabled(sts)
		}))).
		Owns(&corev1.Pod{}, builder.WithPredicates(predicate.NewPredicateFuncs(func(obj client.Object) bool {
			return obj.GetLabels()[consts.LabelComponentKey] == consts.ComponentTypeLogin.String()
		}))).
		Watches(&slurmv1.SlurmCluster{}, handler.EnqueueRequestsFromMapFunc(r.clusterRequests), builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Named(ControllerName).
		WithOptions(controllerconfig.ControllerOptions(maxConcurrency, cacheSyncTimeout)).
		Complete(r)
}
