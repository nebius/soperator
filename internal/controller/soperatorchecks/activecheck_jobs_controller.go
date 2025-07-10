package soperatorchecks

import (
	"context"
	"fmt"
	"time"

	api "github.com/SlinkyProject/slurm-client/api/v0041"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
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
)

type ActiveCheckJobReconciler struct {
	*reconciler.Reconciler
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

		slurmJobID, ok := k8sJob.Annotations["slurm-job-id"]
		if !ok {
			logger.Error(err, "failed to get slurm job id")
			return ctrl.Result{}, err
		}

		slurmJobs, err := slurmAPIClient.GetJobsByID(ctx, slurmJobID)
		if err != nil {
			logger.Error(err, "failed to get slurm job status")
			return ctrl.Result{}, err
		}

		for _, slurmJob := range slurmJobs {
			if !slurmJob.IsTerminalState() {
				return ctrl.Result{Requeue: true, RequeueAfter: 10 * time.Second}, nil
			}
		}

		var failReasons []string
		var jobName string
		var submitTime *metav1.Time
		for _, slurmJob := range slurmJobs {
			if slurmJob.IsFailedState() {
				if slurmJob.StateReason != "" {
					failReasons = append(failReasons, slurmJob.StateReason)
				}

				if activeCheck.Spec.Reactions.DrainSlurmNode {
					nodes, err := slurmJob.GetNodeList()
					if err != nil {
						return ctrl.Result{}, fmt.Errorf("get node list: %w", err)
					}

					reason := consts.SlurmNodeReasonActiveCheckFailedUnknown
					if activeCheck.Spec.Reactions.SetCondition {
						reason = fmt.Sprintf("[HC] Failed %s: job %d [slurm_job]", activeCheckName, slurmJob.ID)
					}
					for _, node := range nodes {
						resp, err := slurmAPIClient.SlurmV0041PostNodeWithResponse(ctx, node,
							api.V0041UpdateNodeMsg{
								Reason: ptr.To(reason),
								State:  ptr.To([]api.V0041UpdateNodeMsgState{api.V0041UpdateNodeMsgStateDRAIN}),
							},
						)
						if err != nil {
							return ctrl.Result{}, fmt.Errorf("post drain slurm node: %w", err)
						}
						if resp.JSON200.Errors != nil && len(*resp.JSON200.Errors) != 0 {
							return ctrl.Result{}, fmt.Errorf("post drain returned errors: %v", *resp.JSON200.Errors)
						}

						logger.V(1).Info("slurm node state is updated to DRAIN")
					}
				}
			}

			if fmt.Sprint(slurmJob.ID) == slurmJobID {
				jobName = slurmJob.Name
				submitTime = slurmJob.SubmitTime
			}
		}

		var state consts.ActiveCheckSlurmJobStatus
		switch {
		case len(failReasons) == 0:
			state = consts.ActiveCheckSlurmJobStatusComplete
		case len(failReasons) == len(slurmJobs):
			state = consts.ActiveCheckSlurmJobStatusFailed
		default:
			state = consts.ActiveCheckSlurmJobStatusDegraded
		}

		activeCheck.Status.SlurmJobsStatus = slurmv1alpha1.ActiveCheckSlurmJobsStatus{
			LastJobId:          slurmJobID,
			LastJobName:        jobName,
			LastJobState:       state,
			LastJobFailReasons: failReasons,
			LastJobSubmitTime:  submitTime,
			LastTransitionTime: metav1.Now(),
		}

		logger = logger.WithValues(logfield.ResourceKV(activeCheck)...)
		logger.V(1).Info("Rendered")

		err = r.Status().Update(ctx, activeCheck)
		if err != nil {
			logger.Error(err, "Failed to reconcile ActiveCheckJob")
			return ctrl.Result{}, fmt.Errorf("reconciling ActiveCheckJob: %w", err)
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
