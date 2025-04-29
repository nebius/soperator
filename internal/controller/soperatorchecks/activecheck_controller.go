package soperatorchecks

import (
	"context"
	"time"

	"github.com/pkg/errors"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	slurmv1 "nebius.ai/slurm-operator/api/v1"
	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/controller/reconciler"
	"nebius.ai/slurm-operator/internal/controllerconfig"
	"nebius.ai/slurm-operator/internal/logfield"
	render "nebius.ai/slurm-operator/internal/render/soperatorchecks"
	"nebius.ai/slurm-operator/internal/utils"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

var (
	SlurmActiveCheckControllerName = "soperatorchecks.activecheck"
)

type ActiveCheckReconciler struct {
	*reconciler.Reconciler
	reconcileTimeout time.Duration

	CronJob *reconciler.CronJobReconciler
}

func NewActiveCheckController(
	client client.Client,
	scheme *runtime.Scheme,
	recorder record.EventRecorder,
	reconcileTimeout time.Duration,
) *ActiveCheckReconciler {
	r := reconciler.NewReconciler(client, scheme, recorder)
	cronJobReconciler := reconciler.NewCronJobReconciler(r)

	return &ActiveCheckReconciler{
		Reconciler:       r,
		reconcileTimeout: reconcileTimeout,
		CronJob:          cronJobReconciler,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *ActiveCheckReconciler) SetupWithManager(
	mgr ctrl.Manager,
	maxConcurrency int,
	cacheSyncTimeout time.Duration,
) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&slurmv1alpha1.ActiveCheck{}).
		WithOptions(controllerconfig.ControllerOptions(maxConcurrency, cacheSyncTimeout)).
		Complete(r)
}

// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=activechecks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=activechecks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=activechecks/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;delete

// Reconcile reconciles all resources necessary for active checks controller
func (r *ActiveCheckReconciler) Reconcile(
	ctx context.Context,
	req ctrl.Request,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName("ActiveCheckController.reconcile")

	logger.Info("Reconciling ActiveCheck", "namespace", req.Namespace, "name", req.Name)

	check := &slurmv1alpha1.ActiveCheck{}
	err := r.Get(ctx, req.NamespacedName, check)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("ActiveCheck resource not found. Ignoring since object must be deleted.")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get ActiveCheck")
		return ctrl.Result{}, err
	}

	activeCheckFinalizer := "slurm.nebius.ai/activecheck-finalizer"
	if check.ObjectMeta.DeletionTimestamp.IsZero() == false {
		if controllerutil.ContainsFinalizer(check, activeCheckFinalizer) {
			logger.Info("ActiveCheck is being deleted. Cleaning up CronJob")
			cronJob := &batchv1.CronJob{}
			err := r.Get(ctx, req.NamespacedName, cronJob)
			if err != nil {
				if !apierrors.IsNotFound(err) {
					logger.Error(err, "Failed to get associated CronJob")
					return ctrl.Result{}, err
				}
				logger.Info("No CronJob found. Nothing to delete")
			} else {
				if err := r.Delete(ctx, cronJob); err != nil {
					logger.Error(err, "Failed to delete associated CronJob")
					return ctrl.Result{}, err
				}
				logger.Info("Deleted associated CronJob")
			}

			controllerutil.RemoveFinalizer(check, activeCheckFinalizer)
			if err := r.Update(ctx, check); err != nil {
				logger.Error(err, "Failed to remove finalizer")
				return ctrl.Result{}, err
			}
		}

		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(check, activeCheckFinalizer) {
		controllerutil.AddFinalizer(check, activeCheckFinalizer)
		if err := r.Update(ctx, check); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
	}

	slurmCluster := &slurmv1.SlurmCluster{}
	slurmClusterNamespacedName := types.NamespacedName{
		Namespace: req.Namespace,
		Name:      check.Spec.SlurmClusterRefName,
	}
	err = r.Get(ctx, slurmClusterNamespacedName, slurmCluster)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.V(1).Info("SlurmCluster resource not found")
		}

		logger.Error(err, "Failed to get SlurmCluster")
		return ctrl.Result{}, errors.Wrap(err, "getting SlurmCluster")
	}

	reconcileActiveChecksImpl := func() error {
		return utils.ExecuteMultiStep(ctx,
			"Reconciliation of active check",
			utils.MultiStepExecutionStrategyFailAtFirstError,

			utils.MultiStepExecutionStep{
				Name: "Active check CronJob",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")

					var foundPodTemplate *corev1.PodTemplate = nil

					if check.Spec.PodTemplateNameRef != nil {
						podTemplateName := *check.Spec.PodTemplateNameRef

						err := r.Get(
							stepCtx,
							types.NamespacedName{
								Namespace: check.Namespace,
								Name:      podTemplateName,
							},
							foundPodTemplate,
						)
						if err != nil {
							stepLogger.Error(err, "Failed to get PodTemplate")
							return errors.Wrap(err, "getting PodTemplate")
						}
					}
					desired, err := render.RenderK8sCronJob(check, foundPodTemplate)

					if err != nil {
						stepLogger.Error(err, "Failed to render")
						return errors.Wrap(err, "rendering Active check CronJob")
					}
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.V(1).Info("Rendered")

					if err = r.CronJob.Reconcile(stepCtx, slurmCluster, &desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling ActiveChecks CronJob")
					}
					stepLogger.V(1).Info("Reconciled")

					if check.Spec.RunAfterCreation && check.Status.K8sJobsStatus.LastTransitionTime.IsZero() {
						if err := r.runAfterCreation(ctx, check, &desired); err != nil {
							stepLogger.Error(err, "Failed to run after creation")
							return errors.Wrap(err, "run job after creation")
						}
					}
					return nil
				},
			},
		)
	}

	if err := reconcileActiveChecksImpl(); err != nil {
		logger.Error(err, "Failed to reconcile ActiveChecks")
		return ctrl.Result{}, errors.Wrap(err, "reconciling ActiveChecks")
	}

	logger.Info("Reconciled ActiveChecks")
	return ctrl.Result{}, nil
}

func (r *ActiveCheckReconciler) runAfterCreation(
	ctx context.Context,
	check *slurmv1alpha1.ActiveCheck,
	cronJob *batchv1.CronJob,
) error {
	jobName := types.NamespacedName{
		Name:      render.RenderK8sJobName(check),
		Namespace: check.Namespace,
	}
	err := r.Client.Get(ctx, jobName, &batchv1.Job{})
	if err != nil && !apierrors.IsNotFound(err) {
		return errors.Wrap(err, "failed to get job")
	}

	if err == nil {
		// Do nothing, job is already present
		return nil
	}

	cronJobName := types.NamespacedName{
		Name:      cronJob.Name,
		Namespace: cronJob.Namespace,
	}
	if err := r.Client.Get(ctx, cronJobName, cronJob); err != nil {
		return errors.Wrap(err, "failed to get cronJob")
	}

	return r.Client.Create(ctx, render.RenderK8sJob(check, cronJob))
}
