package soperatorchecks

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	maintenance "nebius.ai/slurm-operator/internal/check"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/controller/reconciler"
	"nebius.ai/slurm-operator/internal/controllerconfig"
	"nebius.ai/slurm-operator/internal/logfield"
	"nebius.ai/slurm-operator/internal/naming"
	render "nebius.ai/slurm-operator/internal/render/soperatorchecks"
	"nebius.ai/slurm-operator/internal/utils"
)

var (
	SlurmActiveCheckControllerName = "soperatorchecks.activecheck"
)

type ActiveCheckReconciler struct {
	*reconciler.Reconciler
	reconcileTimeout time.Duration

	CronJob   *reconciler.CronJobReconciler
	ConfigMap *reconciler.ConfigMapReconciler
}

func NewActiveCheckController(
	client client.Client,
	scheme *runtime.Scheme,
	recorder record.EventRecorder,
	reconcileTimeout time.Duration,
) *ActiveCheckReconciler {
	r := reconciler.NewReconciler(client, scheme, recorder)
	cronJobReconciler := reconciler.NewCronJobReconciler(r)
	configMapReconciler := reconciler.NewConfigMapReconciler(r)

	return &ActiveCheckReconciler{
		Reconciler:       r,
		reconcileTimeout: reconcileTimeout,
		CronJob:          cronJobReconciler,
		ConfigMap:        configMapReconciler,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *ActiveCheckReconciler) SetupWithManager(
	mgr ctrl.Manager,
	maxConcurrency int,
	cacheSyncTimeout time.Duration,
) error {
	return ctrl.NewControllerManagedBy(mgr).Named(SlurmActiveCheckControllerName).
		For(&slurmv1alpha1.ActiveCheck{}, builder.WithPredicates(
			predicate.Funcs{
				CreateFunc: func(e event.CreateEvent) bool {
					return true
				},
				DeleteFunc: func(e event.DeleteEvent) bool {
					return false
				},
				UpdateFunc: func(e event.UpdateEvent) bool {
					if ac, ok := e.ObjectNew.(*slurmv1alpha1.ActiveCheck); ok {
						return ac.GetDeletionTimestamp() != nil ||
							e.ObjectOld.GetGeneration() != e.ObjectNew.GetGeneration()
					}
					return false
				},
				GenericFunc: func(e event.GenericEvent) bool {
					return false
				},
			},
		)).
		WithOptions(controllerconfig.ControllerOptions(maxConcurrency, cacheSyncTimeout)).
		Complete(r)
}

// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=activechecks,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=activechecks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=activechecks/finalizers,verbs=update
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete

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

	check.Spec.SetDefaults()

	if check.ObjectMeta.DeletionTimestamp.IsZero() == false {
		if controllerutil.ContainsFinalizer(check, consts.ActiveCheckFinalizer) {
			return r.reconcileDelete(ctx, check)
		}

		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(check, consts.ActiveCheckFinalizer) {
		controllerutil.AddFinalizer(check, consts.ActiveCheckFinalizer)
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
			return ctrl.Result{}, fmt.Errorf("SlurmCluster resource not found: %w", err)
		}

		logger.Error(err, "Failed to get SlurmCluster")
		return ctrl.Result{}, fmt.Errorf("getting SlurmCluster: %w", err)
	}

	dependenciesReady, err := r.dependenciesReady(ctx, logger, check, slurmCluster)
	if err != nil {
		return ctrl.Result{}, err
	}
	if !dependenciesReady {
		logger.Info("Not all dependencies are ready, requeueing in 1 minute.")
		return ctrl.Result{
			RequeueAfter: time.Minute,
		}, nil
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

					if check.Spec.SlurmJobSpec.SbatchScript != nil {
						desired := render.RenderSbatchConfigMap(
							check.Spec.Name,
							check.Spec.SlurmClusterRefName,
							check.Namespace,
							*check.Spec.SlurmJobSpec.SbatchScript,
						)
						stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
						stepLogger.V(1).Info("Rendered")

						if err = r.ConfigMap.Reconcile(stepCtx, slurmCluster, &desired); err != nil {
							stepLogger.Error(err, "Failed to reconcile")
							return fmt.Errorf("reconciling ActiveChecks sbatch script ConfigMap: %w", err)
						}
						stepLogger.V(1).Info("Reconciled")
					}

					var foundPodTemplate *corev1.PodTemplate

					if check.Spec.PodTemplateNameRef != nil {
						podTemplateName := *check.Spec.PodTemplateNameRef

						foundPodTemplate = &corev1.PodTemplate{}
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
							return fmt.Errorf("getting PodTemplate: %w", err)
						}
					}
					desired, err := render.RenderK8sCronJob(check, foundPodTemplate)

					if err != nil {
						stepLogger.Error(err, "Failed to render")
						return fmt.Errorf("rendering Active check CronJob: %w", err)
					}
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.V(1).Info("Rendered")

					if err = r.CronJob.Reconcile(stepCtx, slurmCluster, &desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return fmt.Errorf("reconciling ActiveChecks CronJob: %w", err)
					}
					stepLogger.V(1).Info("Reconciled")

					if check.Spec.RunAfterCreation != nil && *check.Spec.RunAfterCreation && check.Status.K8sJobsStatus.LastTransitionTime.IsZero() {
						if err := r.runAfterCreation(ctx, check, &desired); err != nil {
							stepLogger.Error(err, "Failed to run after creation")
							return fmt.Errorf("run job after creation: %w", err)
						}
					}
					return nil
				},
			},
		)
	}

	if err := reconcileActiveChecksImpl(); err != nil {
		logger.Error(err, "Failed to reconcile ActiveChecks")
		return ctrl.Result{}, fmt.Errorf("reconciling ActiveChecks: %w", err)
	}

	logger.Info("Reconciled ActiveChecks")
	return ctrl.Result{}, nil
}

func (r *ActiveCheckReconciler) reconcileDelete(ctx context.Context, check *slurmv1alpha1.ActiveCheck) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName("ActiveCheckController.reconcileDelete")

	logger.Info("ActiveCheck is being deleted. Cleaning up CronJob")
	cronJob := &batchv1.CronJob{}
	err := r.Get(ctx, types.NamespacedName{
		Namespace: check.Namespace,
		Name:      check.Name,
	}, cronJob)
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
	if check.Spec.SlurmJobSpec.SbatchScript != nil {
		logger.Info("ActiveCheck is being deleted. Cleaning up ConfigMap")
		configMap := &corev1.ConfigMap{}
		err = r.Get(ctx, types.NamespacedName{
			Name:      naming.BuildConfigMapSbatchScriptName(check.Spec.Name),
			Namespace: check.Namespace,
		}, configMap)
		if err != nil {
			if !apierrors.IsNotFound(err) {
				logger.Error(err, "Failed to get associated ConfigMap")
				return ctrl.Result{}, err
			}
			logger.Info("No ConfigMap found. Nothing to delete")
		} else {
			if err := r.Delete(ctx, configMap); err != nil {
				logger.Error(err, "Failed to delete associated ConfigMap")
				return ctrl.Result{}, err
			}
			logger.Info("Deleted associated ConfigMap")
		}
	}

	controllerutil.RemoveFinalizer(check, consts.ActiveCheckFinalizer)
	if err := r.Update(ctx, check); err != nil {
		logger.Error(err, "Failed to remove finalizer")
		return ctrl.Result{}, err
	}

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
		return fmt.Errorf("failed to get job: %w", err)
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
		return fmt.Errorf("failed to get cronJob: %w", err)
	}

	return r.Client.Create(ctx, render.RenderK8sJob(check, cronJob))
}

func (r *ActiveCheckReconciler) dependenciesReady(
	ctx context.Context,
	logger logr.Logger,
	check *slurmv1alpha1.ActiveCheck,
	slurmCluster *slurmv1.SlurmCluster,
) (bool, error) {
	if maintenance.IsMaintenanceActive(slurmCluster.Spec.Maintenance) {
		logger.Info(fmt.Sprintf(
			"Slurm cluster maintenance status is %s, skip ActiveCheck reconcile",
			*slurmCluster.Spec.Maintenance,
		))
		return false, nil
	}
	if slurmCluster.Status.Phase == nil || *slurmCluster.Status.Phase != slurmv1.PhaseClusterAvailable {
		logger.Info(fmt.Sprintf(
			"Slurm cluster is not available yet: %s, skip ActiveCheck reconcile",
			*slurmCluster.Status.Phase,
		))
		return false, nil
	}

	for _, prerequisiteCheckName := range check.Spec.DependsOn {
		prerequisiteCheck := &slurmv1alpha1.ActiveCheck{}
		err := r.Get(ctx, types.NamespacedName{
			Namespace: check.Namespace,
			Name:      prerequisiteCheckName,
		}, prerequisiteCheck)
		if err != nil {
			if apierrors.IsNotFound(err) {
				logger.Info(fmt.Sprintf(
					"Prerequisite ActiveCheck %s is not created yet, skip ActiveCheck reconcile",
					prerequisiteCheckName,
				))
				return false, nil
			}
			return false, fmt.Errorf("failed to get prerequisite ActiveCheck: %w", err)
		}

		// TODO: common status?
		switch prerequisiteCheck.Spec.CheckType {
		case "k8sJob": // TODO: const
			if prerequisiteCheck.Status.K8sJobsStatus.LastJobStatus != consts.ActiveCheckK8sJobStatusComplete {
				logger.Info(fmt.Sprintf(
					"Prerequisite ActiveCheck %s is not ready yet, status %s",
					prerequisiteCheckName, prerequisiteCheck.Status.K8sJobsStatus.LastJobStatus,
				))
				return false, nil
			}
		case "slurmJob": // TODO: const
			if prerequisiteCheck.Status.SlurmJobsStatus.LastRunStatus != consts.ActiveCheckSlurmRunStatusComplete {
				logger.Info(fmt.Sprintf(
					"Prerequisite ActiveCheck %s is not ready yet, status %s",
					prerequisiteCheckName, prerequisiteCheck.Status.SlurmJobsStatus.LastRunStatus,
				))
				return false, nil
			}
		default:
			return false, fmt.Errorf("unknown prerequisite ActiveCheck type: %s", prerequisiteCheck.Spec.CheckType)
		}
	}

	return true, nil
}
