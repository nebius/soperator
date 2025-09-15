package soperatorchecks

import (
	"context"
	"fmt"
	"strings"
	"time"

	api "github.com/SlinkyProject/slurm-client/api/v0041"
	"github.com/go-logr/logr"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/slurmapi"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/controller/reconciler"
	"nebius.ai/slurm-operator/internal/controllerconfig"
	"nebius.ai/slurm-operator/internal/logfield"
)

var (
	SlurmActiveCheckJobControllerName = "soperatorchecks.activecheckjob"

	K8sAnnotationSoperatorChecksFinalStateTime = "soperator-checks-final-state-time"
)

type ActiveCheckJobReconciler struct {
	*reconciler.Reconciler
	Job              *reconciler.JobReconciler
	slurmAPIClients  *slurmapi.ClientSet
	reconcileTimeout time.Duration
}

func NewActiveCheckJobController(
	client client.Client,
	scheme *runtime.Scheme,
	recorder record.EventRecorder,
	slurmAPIClients *slurmapi.ClientSet,
	reconcileTimeout time.Duration,
) *ActiveCheckJobReconciler {
	r := reconciler.NewReconciler(client, scheme, recorder)

	return &ActiveCheckJobReconciler{
		Reconciler:       r,
		Job:              reconciler.NewJobReconciler(r),
		slurmAPIClients:  slurmAPIClients,
		reconcileTimeout: reconcileTimeout,
	}
}

func (r *ActiveCheckJobReconciler) SetupWithManager(mgr ctrl.Manager,
	maxConcurrency int, cacheSyncTimeout time.Duration) error {
	return ctrl.NewControllerManagedBy(mgr).Named(SlurmActiveCheckJobControllerName).
		For(&batchv1.Job{}, builder.WithPredicates(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				job, ok := e.Object.(*batchv1.Job)
				if !ok {
					return false
				}

				return isValidJob(job)
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				job, ok := e.ObjectNew.(*batchv1.Job)
				if !ok {
					return false
				}

				return isValidJob(job)
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				return false
			},
			GenericFunc: func(e event.GenericEvent) bool {
				job, ok := e.Object.(*batchv1.Job)
				if !ok {
					return false
				}

				return isValidJob(job)
			},
		})).
		WithOptions(controllerconfig.ControllerOptions(maxConcurrency, cacheSyncTimeout)).
		Complete(r)
}

func isValidJob(cm *batchv1.Job) bool {
	if v, ok := cm.Labels[consts.LabelComponentKey]; ok {
		return v == consts.ComponentTypeSoperatorChecks.String()
	}
	return false
}

// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=activechecks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=activechecks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=activechecks/finalizers,verbs=update

// Reconcile reconciles all resources necessary for active checks controller
func (r *ActiveCheckJobReconciler) Reconcile(
	ctx context.Context,
	req ctrl.Request,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName("ActiveCheckJobReconciler.reconcile")

	logger.Info("Reconciling ActiveCheckJob", "namespace", req.Namespace, "name", req.Name)

	k8sJob := &batchv1.Job{}
	err := r.Get(ctx, req.NamespacedName, k8sJob)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("ActiveCheckJob resource not found. Ignoring since object must be deleted.")
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get active check job: %w", err)
	}

	activeCheckName, err := r.getActiveCheckNameFromJob(ctx, k8sJob)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("get active check name: %w", err)
	}
	activeCheck := &slurmv1alpha1.ActiveCheck{}
	err = r.Get(ctx, types.NamespacedName{
		Namespace: req.Namespace,
		Name:      activeCheckName,
	}, activeCheck)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("ActiveCheck resource not found. Ignoring since object must be deleted.")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get ActiveCheck")
		return ctrl.Result{}, err
	}

	cronJob := &batchv1.CronJob{}
	err = r.Get(ctx, types.NamespacedName{
		Namespace: req.Namespace,
		Name:      activeCheckName,
	}, cronJob)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("CronJob resource not found. Ignoring since object must be deleted.")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get CronJob")
		return ctrl.Result{}, err
	}

	if activeCheck.Spec.CheckType == "slurmJob" {
		slurmClusterName := types.NamespacedName{
			Namespace: req.Namespace,
			Name:      activeCheck.Spec.SlurmClusterRefName,
		}
		slurmAPIClient, found := r.slurmAPIClients.GetClient(slurmClusterName)
		if !found {
			logger.Error(err, "failed to get slurm api client")
			return ctrl.Result{}, fmt.Errorf("slurm cluster %v not found", slurmClusterName)
		}

		slurmJobIDs, ok := k8sJob.Annotations["slurm-job-id"]
		if !ok {
			logger.Error(err, "failed to get slurm job id")
			return ctrl.Result{}, err
		}
		ids := strings.Split(slurmJobIDs, ",")
		firstJobId := ids[0]

		var jobName string
		var submitTime *metav1.Time
		var failJobsAndReasons []slurmv1alpha1.JobAndReason
		var degradeJobsAndReasons []slurmv1alpha1.JobAndReason
		if activeCheck.Status.SlurmJobsStatus.LastRunId == firstJobId {
			failJobsAndReasons = activeCheck.Status.SlurmJobsStatus.LastRunFailJobsAndReasons
			degradeJobsAndReasons = activeCheck.Status.SlurmJobsStatus.LastRunDegradeJobsAndReasons
		}
		requeue := false
		final := false
		totalJobs := 0
		for _, slurmJobID := range ids {
			slurmJobs, err := slurmAPIClient.GetJobsByID(ctx, slurmJobID)
			if err != nil {
				logger.Error(err, "failed to get slurm job status")
				return ctrl.Result{}, err
			}
			totalJobs += len(slurmJobs)

			for _, slurmJob := range slurmJobs {
				if fmt.Sprint(slurmJob.ID) == firstJobId {
					jobName = slurmJob.Name
					submitTime = slurmJob.SubmitTime
				}

				// Job is not yet finished
				if !slurmJob.IsTerminalState() || slurmJob.EndTime == nil {
					requeue = true
					continue
				}

				// Job has already been seen in one of the previous reconciler runs
				if k8sJob.Annotations[K8sAnnotationSoperatorChecksFinalStateTime] != "" {
					continue
				}

				switch {
				case slurmJob.IsFailedState():
					failJobsAndReasons = append(failJobsAndReasons, slurmv1alpha1.JobAndReason{
						JobID:  fmt.Sprint(slurmJob.ID),
						Reason: slurmJob.StateReason,
					})
					err = executeFailureReactions(ctx, slurmJob, activeCheck, slurmAPIClient, logger)
					if err != nil {
						return ctrl.Result{}, fmt.Errorf("executing failure reactions: %w", err)
					}
				case slurmJob.IsCompletedState():
					err = executeSuccessReactions(ctx, slurmJob, activeCheck, slurmAPIClient)
					if err != nil {
						return ctrl.Result{}, fmt.Errorf("executing success reactions: %w", err)
					}
				default:
					degradeJobsAndReasons = append(failJobsAndReasons, slurmv1alpha1.JobAndReason{
						JobID:  fmt.Sprint(slurmJob.ID),
						Reason: slurmJob.StateReason,
					})
					// Do nothing. The job could have been cancelled or interrupted. The job will run again.
					logger.Info(fmt.Sprintf("unhandled state. The job is probably cancelled or interrupted and it will run again. Current state: %s ", slurmJob.State))
				}
				final = true
			}
		}

		if final {
			// Maybe we could delete the job because it will not be processed anymore
			// Otherwise, we will have many of these jobs and they will keep being listed in every Reconcile()
			k8sJobPatch := client.MergeFrom(k8sJob.DeepCopy())
			k8sJob.Annotations[K8sAnnotationSoperatorChecksFinalStateTime] = fmt.Sprintf("%d", time.Now().Unix())
			if err := r.Job.Patch(ctx, k8sJob, k8sJobPatch); err != nil {
				return ctrl.Result{}, fmt.Errorf("failed to patch k8s Job: %w", err)
			}
		}

		// Updating the ActiveCheck status is relevant for normal active checks
		// where there is only one instance of the ActiveCheck running at any moment.
		// For extensive-check, there will be many instances running at the same time.
		// It doesn't really make sense to update the status of the active check.
		// Leaving this logic as it is for now.

		var state consts.ActiveCheckSlurmRunStatus
		switch {
		case requeue:
			state = consts.ActiveCheckSlurmRunStatusInProgress
		case len(failJobsAndReasons) != 0:
			state = consts.ActiveCheckSlurmRunStatusFailed
		case len(degradeJobsAndReasons) != 0:
			state = consts.ActiveCheckSlurmRunStatusDegraded
		default:
			state = consts.ActiveCheckSlurmRunStatusComplete
		}

		activeCheck.Status.SlurmJobsStatus = slurmv1alpha1.ActiveCheckSlurmJobsStatus{
			LastRunId:                    firstJobId,
			LastRunName:                  jobName,
			LastRunStatus:                state,
			LastRunFailJobsAndReasons:    failJobsAndReasons,
			LastRunDegradeJobsAndReasons: degradeJobsAndReasons,
			LastRunSubmitTime:            submitTime,
			LastTransitionTime:           metav1.Now(),
		}

		logger = logger.WithValues(logfield.ResourceKV(activeCheck)...)
		logger.V(1).Info("Rendered")

		err = r.Status().Update(ctx, activeCheck)
		if err != nil {
			logger.Error(err, "Failed to reconcile ActiveCheckJob")
			return ctrl.Result{}, fmt.Errorf("reconciling ActiveCheckJob: %w", err)
		}

		if requeue {
			return ctrl.Result{Requeue: true, RequeueAfter: 10 * time.Second}, nil
		}

	} else if activeCheck.Spec.CheckType == "k8sJob" {
		newStatus := slurmv1alpha1.ActiveCheckK8sJobsStatus{
			LastJobScheduleTime:   cronJob.Status.LastScheduleTime,
			LastJobSuccessfulTime: cronJob.Status.LastSuccessfulTime,

			LastJobName:   k8sJob.Name,
			LastJobStatus: getK8sJobStatus(k8sJob),
		}

		newStatusCopy := newStatus.DeepCopy()
		currentStatusCopy := activeCheck.Status.K8sJobsStatus.DeepCopy()
		newStatusCopy.LastTransitionTime = metav1.Time{}
		currentStatusCopy.LastTransitionTime = metav1.Time{}

		if *newStatusCopy == *currentStatusCopy {
			logger.Info("Reconciled ActiveCheckJob, no update were made")
			return ctrl.Result{}, nil
		}

		newStatus.LastTransitionTime = metav1.Now()
		activeCheck.Status.K8sJobsStatus = newStatus

		logger = logger.WithValues(logfield.ResourceKV(activeCheck)...)
		logger.V(1).Info("Rendered")

		err = r.Status().Update(ctx, activeCheck)
		if err != nil {
			logger.Error(err, "Failed to reconcile ActiveCheckJob")
			return ctrl.Result{}, fmt.Errorf("reconciling ActiveCheckJob: %w", err)
		}
	}

	logger.Info("Reconciled ActiveCheckJob")
	return ctrl.Result{}, nil
}

func updateSlurmNodeWithReaction(
	ctx context.Context,
	logger logr.Logger,
	slurmJob slurmapi.Job,
	activeCheck *slurmv1alpha1.ActiveCheck,
	slurmAPIClient slurmapi.Client,
) error {
	nodes, err := slurmJob.GetNodeList()
	if err != nil {
		return fmt.Errorf("get node list: %w", err)
	}

	failureMessage := fmt.Sprintf("[node_problem] Failed %s: job %d [slurm_job]", activeCheck.Name, slurmJob.ID)
	reason := consts.SlurmNodeReasonActiveCheckFailedUnknown
	if activeCheck.Spec.Reactions.SetCondition {
		reason = failureMessage
	}
	for _, node := range nodes {
		updateReq := api.V0041UpdateNodeMsg{}
		if activeCheck.Spec.Reactions.DrainSlurmNode {
			updateReq.Reason = ptr.To(reason)
			updateReq.State = ptr.To([]api.V0041UpdateNodeMsgState{api.V0041UpdateNodeMsgStateDRAIN})
		}
		if activeCheck.Spec.Reactions.CommentSlurmNode {
			updateReq.Comment = ptr.To(failureMessage)
		}

		resp, err := slurmAPIClient.SlurmV0041PostNodeWithResponse(ctx, node, updateReq)
		if err != nil {
			return fmt.Errorf("post update slurm node: %w", err)
		}
		if resp.JSON200.Errors != nil && len(*resp.JSON200.Errors) != 0 {
			return fmt.Errorf("post update returned errors: %v", *resp.JSON200.Errors)
		}

		logger.V(1).Info(fmt.Sprintf("slurm node is updated, drain: %t, comment %t",
			activeCheck.Spec.Reactions.DrainSlurmNode, activeCheck.Spec.Reactions.CommentSlurmNode))
	}

	return nil
}

func (r *ActiveCheckJobReconciler) getActiveCheckNameFromJob(ctx context.Context, k8sJob *batchv1.Job) (string, error) {
	podList := &corev1.PodList{}
	err := r.List(ctx, podList, client.InNamespace(k8sJob.Namespace), client.MatchingLabels{"job-name": k8sJob.Name})
	if err != nil || len(podList.Items) == 0 {
		return "", fmt.Errorf("failed to find pod for job %s: %w", k8sJob.Name, err)
	}

	pod := &podList.Items[0]
	activeCheckName, ok := pod.Annotations[consts.AnnotationActiveCheckKey]
	if !ok {
		return "", fmt.Errorf("annotation %s not found on pod %s", consts.AnnotationActiveCheckKey, pod.Name)
	}

	return activeCheckName, nil
}

func getK8sJobStatus(k8sJob *batchv1.Job) consts.ActiveCheckK8sJobStatus {
	status := k8sJob.Status

	if status.Active > 0 {
		return consts.ActiveCheckK8sJobStatusActive
	}

	for _, condition := range status.Conditions {
		if condition.Status == corev1.ConditionTrue {
			switch condition.Type {
			case batchv1.JobComplete, batchv1.JobSuccessCriteriaMet:
				return consts.ActiveCheckK8sJobStatusComplete
			case batchv1.JobFailed, batchv1.JobFailureTarget:
				return consts.ActiveCheckK8sJobStatusFailed
			case batchv1.JobSuspended:
				return consts.ActiveCheckK8sJobStatusSuspended
			}
		}
	}

	if status.Active == 0 && len(status.Conditions) == 0 {
		return consts.ActiveCheckK8sJobStatusPending
	}

	return consts.ActiveCheckK8sJobStatusUnknown
}

func executeFailureReactions(ctx context.Context, slurmJob slurmapi.Job, activeCheck *slurmv1alpha1.ActiveCheck, slurmAPIClient slurmapi.Client, logger logr.Logger) error {
	failureReactions := activeCheck.Spec.FailureReactions
	if failureReactions == nil {
		failureReactions = &activeCheck.Spec.Reactions
	}

	if failureReactions.DrainSlurmNode || failureReactions.CommentSlurmNode {
		err := updateSlurmNodeWithReaction(ctx, logger, slurmJob, activeCheck, slurmAPIClient)
		if err != nil {
			return fmt.Errorf("update slurm node with reaction: %w", err)
		}
	}

	err := processAddReservation(ctx, failureReactions.AddReservation, slurmJob, slurmAPIClient, logger)
	if err != nil {
		return fmt.Errorf("adding reservation: %w", err)
	}
	return nil
}

func executeSuccessReactions(ctx context.Context, slurmJob slurmapi.Job, activeCheck *slurmv1alpha1.ActiveCheck, slurmAPIClient slurmapi.Client) error {
	successReactions := activeCheck.Spec.SuccessReactions
	if successReactions == nil {
		return nil
	}

	err := processRemoveReservation(ctx, successReactions.RemoveReservation, slurmJob, slurmAPIClient)
	if err != nil {
		return fmt.Errorf("removing reservation: %w", err)
	}
	return nil
}

func processAddReservation(ctx context.Context, addReservation *slurmv1alpha1.ReservationSpec, slurmJob slurmapi.Job, slurmAPIClient slurmapi.Client, logger logr.Logger) error {
	if addReservation == nil || addReservation.Prefix == "" {
		return nil
	}

	nodes, err := slurmJob.GetNodeList()
	if err != nil {
		return fmt.Errorf("get node list: %w", err)
	}
	for _, node := range nodes {
		err := addReservationForNode(ctx, addReservation.Prefix, node, slurmAPIClient, logger)
		if err != nil {
			return fmt.Errorf("post reservation: %w", err)
		}
	}
	return nil
}

func processRemoveReservation(ctx context.Context, removeReservation *slurmv1alpha1.ReservationSpec, slurmJob slurmapi.Job, slurmAPIClient slurmapi.Client) error {
	if removeReservation == nil || removeReservation.Prefix == "" {
		return nil
	}

	nodes, err := slurmJob.GetNodeList()
	if err != nil {
		return fmt.Errorf("get node list: %w", err)
	}
	for _, node := range nodes {
		reservationName := naming.BuildSlurmReservationNameForNode(removeReservation.Prefix, node)
		err := slurmAPIClient.StopReservation(ctx, reservationName)
		if err != nil {
			return fmt.Errorf("stop reservation: %w", err)
		}
	}

	return nil
}

func addReservationForNode(ctx context.Context, reservationPrefix string, nodeName string, slurmAPIClient slurmapi.Client, logger logr.Logger) error {
	reservationName := naming.BuildSlurmReservationNameForNode(reservationPrefix, nodeName)

	_, err := slurmAPIClient.GetReservation(ctx, reservationName)
	if err == nil {
		// Reservation is found, don't create a new one
		logger.Info("previous reservation was found. Not creating a new reservation", "name", reservationName)
		return nil
	}

	logger.Info("adding reservation for node", "name", reservationName)
	return slurmAPIClient.PostMaintenanceReservation(ctx, reservationName, []string{nodeName})
}
