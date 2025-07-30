package clustercontroller

import (
	"context"
	errorsStd "errors"
	"fmt"
	"time"

	mariadbv1alpha1 "github.com/mariadb-operator/mariadb-operator/api/v1alpha1"
	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"
	prometheusv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	apparmor "sigs.k8s.io/security-profiles-operator/api/apparmorprofile/v1alpha1"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/check"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/controller/reconciler"
	"nebius.ai/slurm-operator/internal/controller/state"
	"nebius.ai/slurm-operator/internal/controllerconfig"
	"nebius.ai/slurm-operator/internal/logfield"
	"nebius.ai/slurm-operator/internal/utils"
	"nebius.ai/slurm-operator/internal/values"
)

//+kubebuilder:rbac:groups=slurm.nebius.ai,resources=slurmclusters,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=slurm.nebius.ai,resources=slurmclusters/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=slurm.nebius.ai,resources=slurmclusters/finalizers,verbs=update
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps,resources=statefulsets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=events,verbs=create;patch
//+kubebuilder:rbac:groups=core,resources=pods,verbs=create;delete;get;list;patch;update;watch
//+kubebuilder:rbac:groups=core,resources=services,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=persistentvolumeclaims,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch,resources=jobs/status,verbs=get;update
//+kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch,resources=cronjobs/status,verbs=get;update
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles,verbs=get;list;watch;create;delete;patch
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=rolebindings,verbs=get;list;watch;create;delete;patch
//+kubebuilder:rbac:groups=apps.kruise.io,resources=statefulsets,verbs=get;list;watch;update;patch;delete;create
//+kubebuilder:rbac:groups=monitoring.coreos.com,resources=podmonitors,verbs=get;list;watch;update;patch;delete;create
//+kubebuilder:rbac:groups=core,resources=podtemplates,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;update;patch;delete;create
//+kubebuilder:rbac:groups=k8s.mariadb.com,resources=mariadbs,verbs=get;list;watch;update;patch;delete;create
//+kubebuilder:rbac:groups=k8s.mariadb.com,resources=grants,verbs=get;list;watch;update;patch;delete;create
//+kubebuilder:rbac:groups=apps,resources=daemonsets,verbs=get;list;watch;update;patch;delete;create
//+kubebuilder:rbac:groups=security-profiles-operator.x-k8s.io,resources=apparmorprofiles,verbs=get;list;watch;update;patch;delete;create
//+kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;list;watch;create;update;patch;
//+kubebuilder:rbac:groups=slurm.nebius.ai,resources=jailedconfigs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=slurm.nebius.ai,resources=jailedconfigs/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=slurm.nebius.ai,resources=jailedconfigs/finalizers,verbs=update

// SlurmClusterReconciler reconciles a SlurmCluster object
type SlurmClusterReconciler struct {
	*reconciler.Reconciler

	ConfigMap           *reconciler.ConfigMapReconciler
	JailedConfig        *reconciler.JailedConfigReconciler
	Secret              *reconciler.SecretReconciler
	CronJob             *reconciler.CronJobReconciler
	Job                 *reconciler.JobReconciler
	Service             *reconciler.ServiceReconciler
	StatefulSet         *reconciler.StatefulSetReconciler
	AdvancedStatefulSet *reconciler.AdvancedStatefulSetReconciler
	ServiceAccount      *reconciler.ServiceAccountReconciler
	Role                *reconciler.RoleReconciler
	RoleBinding         *reconciler.RoleBindingReconciler
	PodMonitor          *reconciler.PodMonitorReconciler
	Deployment          *reconciler.DeploymentReconciler
	MariaDb             *reconciler.MariaDbReconciler
	MariaDbGrant        *reconciler.MariaDbGrantReconciler
	AppArmorProfile     *reconciler.AppArmorProfileReconciler
}

func NewSlurmClusterReconciler(client client.Client, scheme *runtime.Scheme, recorder record.EventRecorder) *SlurmClusterReconciler {
	r := reconciler.NewReconciler(client, scheme, recorder)
	return &SlurmClusterReconciler{
		Reconciler:          r,
		ConfigMap:           reconciler.NewConfigMapReconciler(r),
		JailedConfig:        reconciler.NewJailedConfigReconciler(r),
		Secret:              reconciler.NewSecretReconciler(r),
		CronJob:             reconciler.NewCronJobReconciler(r),
		Job:                 reconciler.NewJobReconciler(r),
		Service:             reconciler.NewServiceReconciler(r),
		StatefulSet:         reconciler.NewStatefulSetReconciler(r),
		AdvancedStatefulSet: reconciler.NewAdvancedStatefulSetReconciler(r),
		ServiceAccount:      reconciler.NewServiceAccountReconciler(r),
		Role:                reconciler.NewRoleReconciler(r),
		RoleBinding:         reconciler.NewRoleBindingReconciler(r),
		PodMonitor:          reconciler.NewPodMonitorReconciler(r),
		Deployment:          reconciler.NewDeploymentReconciler(r),
		MariaDb:             reconciler.NewMariaDbReconciler(r),
		MariaDbGrant:        reconciler.NewMariaDbGrantReconciler(r),
		AppArmorProfile:     reconciler.NewAppArmorProfileReconciler(r),
	}
}

// Reconcile implements the reconciling logic for the Slurm Cluster.
// The reconciling cycle is actually implemented in the auxiliary 'reconcile' method
func (r *SlurmClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues(
		logfield.ClusterNamespace, req.Namespace,
		logfield.ClusterName, req.Name,
		logfield.ResourceKind, slurmv1.KindSlurmCluster,
		logfield.ResourceName, req.Name,
	)
	log.IntoContext(ctx, logger)

	slurmCluster := &slurmv1.SlurmCluster{}
	err := r.Get(ctx, req.NamespacedName, slurmCluster)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.V(1).Info("SlurmCluster resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		logger.Error(err, "Failed to get SlurmCluster")
		return ctrl.Result{Requeue: true}, fmt.Errorf("getting SlurmCluster: %w", err)
	}

	// If cluster marked for deletion, we have nothing to do
	if slurmCluster.GetDeletionTimestamp() != nil {
		return ctrl.Result{}, nil
	}

	result, err := r.reconcile(ctx, slurmCluster)
	if err != nil {
		logger.Error(err, "Failed to reconcile SlurmCluster")
		result = ctrl.Result{}
		err = fmt.Errorf("reconciling SlurmCluster: %w", err)
	}

	statusErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cluster := &slurmv1.SlurmCluster{}
		innerErr := r.Get(ctx, req.NamespacedName, cluster)
		if innerErr != nil {
			if apierrors.IsNotFound(innerErr) {
				logger.V(1).Info("SlurmCluster resource not found. Ignoring since object must be deleted")
				return nil
			}
			// Error reading the object - requeue the request.
			logger.Error(innerErr, "Failed to get SlurmCluster")
			return fmt.Errorf("getting SlurmCluster: %w", innerErr)
		}

		return r.Status().Update(ctx, cluster)
	})
	if statusErr != nil {
		logger.Error(statusErr, "Failed to update SlurmCluster status")
		result = ctrl.Result{}
		err = fmt.Errorf("updating SlurmCluster status: %w", statusErr)
	}

	return result, errorsStd.Join(err, statusErr)
}

func (r *SlurmClusterReconciler) reconcile(ctx context.Context, cluster *slurmv1.SlurmCluster) (ctrl.Result, error) {
	// TODO: debug where logger comes from.
	logger := log.FromContext(ctx)

	{
		kind := cluster.GetObjectKind()
		key := client.ObjectKeyFromObject(cluster)
		if state.ReconciliationState.Present(kind, key) {
			logger.V(1).Info("Reconciliation skipped, as object is already present in reconciliation state",
				"kind", kind.GroupVersionKind().String(),
				"key", key.String(),
			)
			return ctrl.Result{}, nil
		}

		state.ReconciliationState.Set(kind, key)
		logger.V(1).Info("Reconciliation state set for object",
			"kind", kind.GroupVersionKind().String(),
			"key", key.String(),
		)

		defer func() {
			state.ReconciliationState.Remove(kind, key)
			logger.V(1).Info("Reconciliation state removed for object",
				"kind", kind.GroupVersionKind().String(),
				"key", key.String(),
			)
		}()
	}

	logger.Info("Starting reconciliation of Slurm Cluster")

	clusterValues, err := values.BuildSlurmClusterFrom(ctx, cluster)
	if err != nil {
		return ctrl.Result{}, err
	}

	if err = r.setUpConditions(ctx, cluster); err != nil {
		return ctrl.Result{}, err
	}

	// Reconciliation
	res, err := r.runWithPhase(ctx, cluster,
		ptr.To(slurmv1.PhaseClusterReconciling),
		func() (ctrl.Result, error) {
			if !check.IsModeSkipPopulateJail(clusterValues.PopulateJail.Maintenance) {
				if err = r.ReconcilePopulateJail(ctx, clusterValues, cluster); err != nil {
					return ctrl.Result{}, err
				}
			}
			if err = r.ReconcileCommon(ctx, cluster, clusterValues); err != nil {
				return ctrl.Result{}, err
			}
			if err = r.ReconcileAccounting(ctx, cluster, clusterValues); err != nil {
				return ctrl.Result{}, err
			}
			if err = r.ReconcileWorkers(ctx, cluster, clusterValues); err != nil {
				return ctrl.Result{}, err
			}
			if err = r.ReconcileSConfigController(ctx, cluster, clusterValues); err != nil {
				return ctrl.Result{}, err
			}
			if err = r.ReconcileControllers(ctx, cluster, clusterValues); err != nil {
				return ctrl.Result{}, err
			}
			// TODO: Drop this check because Slurm clusters can't exist without Login nodes
			if clusterValues.NodeLogin.Size > 0 {
				if err = r.ReconcileLogin(ctx, cluster, clusterValues); err != nil {
					return ctrl.Result{}, err
				}
			}
			if err = r.ReconcileREST(ctx, cluster, clusterValues); err != nil {
				return ctrl.Result{}, err
			}
			if err = r.ReconcileExporter(ctx, cluster, clusterValues); err != nil {
				return ctrl.Result{}, err
			}
			if err = r.ReconcileSoperatorExporter(ctx, cluster, clusterValues); err != nil {
				return ctrl.Result{}, err
			}

			logger.Info("Reconciled Slurm Cluster")

			return ctrl.Result{}, nil
		},
	)
	if err != nil {
		return ctrl.Result{}, err
	} else if res.Requeue {
		return res, nil
	}

	// Validation
	res, err = r.runWithPhase(ctx, cluster,
		ptr.To(slurmv1.PhaseClusterNotAvailable),
		func() (ctrl.Result, error) {
			// Common
			switch {
			case check.IsMaintenanceActive(clusterValues.NodeRest.Maintenance):
				err = r.patchStatus(ctx, cluster, func(status *slurmv1.SlurmClusterStatus) {
					status.SetCondition(metav1.Condition{
						Type:    slurmv1.ConditionClusterCommonAvailable,
						Status:  metav1.ConditionFalse,
						Reason:  "Maintenance",
						Message: "Slurm common components are in maintenance",
					})
				})
				if err != nil {
					return ctrl.Result{}, err
				}
			default:
				if err = r.patchStatus(ctx, cluster, func(status *slurmv1.SlurmClusterStatus) {
					status.SetCondition(metav1.Condition{
						Type:   slurmv1.ConditionClusterCommonAvailable,
						Status: metav1.ConditionTrue, Reason: "Available",
						Message: "Slurm common components are available",
					})
				}); err != nil {
					return ctrl.Result{}, err
				}
			}

			// Populate Jail
			switch {
			case check.IsModeSkipPopulateJail(clusterValues.PopulateJail.Maintenance):
				if err = r.patchStatus(ctx, cluster, func(status *slurmv1.SlurmClusterStatus) {
					status.SetCondition(metav1.Condition{
						Type:   slurmv1.ConditionClusterPopulateJailMode,
						Status: metav1.ConditionTrue, Reason: string(consts.ModeSkipPopulate),
						Message: "Populate Jail is skipped",
					})
				}); err != nil {
					return ctrl.Result{}, err
				}
			case check.IsModeDownscaleAndDeletePopulate(clusterValues.PopulateJail.Maintenance):
				if err = r.patchStatus(ctx, cluster, func(status *slurmv1.SlurmClusterStatus) {
					status.SetCondition(metav1.Condition{
						Type:   slurmv1.ConditionClusterPopulateJailMode,
						Status: metav1.ConditionTrue, Reason: string(consts.ModeDownscaleAndDeletePopulate),
						Message: "Populate Jail is deleted",
					})
				}); err != nil {
					return ctrl.Result{}, err
				}
			case check.IsModeDownscaleAndOverwritePopulate(clusterValues.PopulateJail.Maintenance):
				if err = r.patchStatus(ctx, cluster, func(status *slurmv1.SlurmClusterStatus) {
					status.SetCondition(metav1.Condition{
						Type:   slurmv1.ConditionClusterPopulateJailMode,
						Status: metav1.ConditionTrue, Reason: string(consts.ModeDownscaleAndOverwritePopulate),
						Message: "Populate Jail is overwritten",
					})
				}); err != nil {
					return ctrl.Result{}, err
				}
			case !check.IsMaintenanceActive(clusterValues.PopulateJail.Maintenance):
				if err = r.patchStatus(ctx, cluster, func(status *slurmv1.SlurmClusterStatus) {
					status.SetCondition(metav1.Condition{
						Type:   slurmv1.ConditionClusterPopulateJailMode,
						Status: metav1.ConditionTrue, Reason: string(consts.ModeNone),
						Message: fmt.Sprintf("Populate Jail maintenanceMode is %s", consts.ModeNone),
					})
				}); err != nil {
					return ctrl.Result{}, err
				}
			default:
				if err = r.patchStatus(ctx, cluster, func(status *slurmv1.SlurmClusterStatus) {
					status.SetCondition(metav1.Condition{
						Type:   slurmv1.ConditionClusterPopulateJailMode,
						Status: metav1.ConditionUnknown, Reason: "Unknown",
						Message: "Unknown Populate Jail maintenanceMode",
					})
				}); err != nil {
					return ctrl.Result{}, err
				}
			}

			// Controllers
			switch {
			case check.IsMaintenanceActive(clusterValues.NodeController.Maintenance):
				err = r.patchStatus(ctx, cluster, func(status *slurmv1.SlurmClusterStatus) {
					status.SetCondition(metav1.Condition{
						Type:    slurmv1.ConditionClusterControllersAvailable,
						Status:  metav1.ConditionFalse,
						Reason:  "Maintenance",
						Message: "Slurm controllers are in maintenance",
					})
				})
				if err != nil {
					return ctrl.Result{}, err
				}
			default:
				if res, err := r.ValidateControllers(ctx, cluster, clusterValues); err != nil {
					logger.Error(err, "Failed to validate Slurm controllers")
					return ctrl.Result{}, fmt.Errorf("validating Slurm controllers: %w", err)
				} else if res.Requeue {
					return res, nil
				}
			}

			// Workers
			switch {
			case check.IsMaintenanceActive(clusterValues.NodeWorker.Maintenance):
				err = r.patchStatus(ctx, cluster, func(status *slurmv1.SlurmClusterStatus) {
					status.SetCondition(metav1.Condition{
						Type:    slurmv1.ConditionClusterWorkersAvailable,
						Status:  metav1.ConditionFalse,
						Reason:  "Maintenance",
						Message: "Slurm workers are in maintenance",
					})
				})
				if err != nil {
					return ctrl.Result{}, err
				}

			case clusterValues.NodeWorker.Size > 0:
				if res, err := r.ValidateWorkers(ctx, cluster, clusterValues); err != nil {
					logger.Error(err, "Failed to validate Slurm workers")
					return ctrl.Result{}, fmt.Errorf("validating Slurm workers: %w", err)
				} else if res.Requeue {
					return res, nil
				}
			default:
				if err = r.patchStatus(ctx, cluster, func(status *slurmv1.SlurmClusterStatus) {
					status.SetCondition(metav1.Condition{
						Type:   slurmv1.ConditionClusterWorkersAvailable,
						Status: metav1.ConditionFalse, Reason: "NotAvailable",
						Message: "Slurm workers are disabled",
					})
				}); err != nil {
					return ctrl.Result{}, err
				}
			}

			// Login
			switch {
			case check.IsMaintenanceActive(clusterValues.NodeLogin.Maintenance):
				err = r.patchStatus(ctx, cluster, func(status *slurmv1.SlurmClusterStatus) {
					status.SetCondition(metav1.Condition{
						Type:    slurmv1.ConditionClusterLoginAvailable,
						Status:  metav1.ConditionFalse,
						Reason:  "Maintenance",
						Message: "Slurm login is in maintenance",
					})
				})
				if err != nil {
					return ctrl.Result{}, err
				}

			case clusterValues.NodeLogin.Size > 0:
				if res, err := r.ValidateLogin(ctx, cluster, clusterValues); err != nil {
					logger.Error(err, "Failed to validate Slurm login")
					return ctrl.Result{}, fmt.Errorf("validating Slurm login: %w", err)
				} else if res.Requeue {
					return res, nil
				}
			default:
				if err = r.patchStatus(ctx, cluster, func(status *slurmv1.SlurmClusterStatus) {
					status.SetCondition(metav1.Condition{
						Type:   slurmv1.ConditionClusterLoginAvailable,
						Status: metav1.ConditionFalse, Reason: "NotAvailable",
						Message: "Slurm Login is disabled",
					})
				}); err != nil {
					return ctrl.Result{}, err
				}
			}

			// Accounting
			switch {
			case check.IsMaintenanceActive(clusterValues.NodeAccounting.Maintenance):
				err = r.patchStatus(ctx, cluster, func(status *slurmv1.SlurmClusterStatus) {
					status.SetCondition(metav1.Condition{
						Type:    slurmv1.ConditionClusterAccountingAvailable,
						Status:  metav1.ConditionFalse,
						Reason:  "Maintenance",
						Message: "Slurm accounting is in maintenance",
					})
				})
				if err != nil {
					return ctrl.Result{}, err
				}

			case !clusterValues.NodeAccounting.Enabled:
				err = r.patchStatus(ctx, cluster, func(status *slurmv1.SlurmClusterStatus) {
					status.SetCondition(metav1.Condition{
						Type:    slurmv1.ConditionClusterAccountingAvailable,
						Status:  metav1.ConditionFalse,
						Reason:  "NotAvailable",
						Message: "Slurm accounting is disabled",
					})
				})
				if err != nil {
					return ctrl.Result{}, err
				}

			default:
				res, err := r.ValidateAccounting(ctx, cluster, clusterValues)
				if err != nil {
					logger.Error(err, "Failed to validate Slurm accounting")
					return ctrl.Result{}, fmt.Errorf("validating Slurm accounting: %w", err)
				}
				if res.Requeue {
					return res, nil
				}
			}

			return ctrl.Result{}, nil
		},
	)
	if err != nil {
		return ctrl.Result{}, err
	} else if res.Requeue {
		return res, nil
	}

	// Availability
	if _, err = r.runWithPhase(ctx, cluster,
		ptr.To(slurmv1.PhaseClusterAvailable),
		func() (ctrl.Result, error) { return ctrl.Result{}, nil },
	); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("Finished reconciliation of Slurm Cluster")

	return ctrl.Result{}, nil
}

func (r *SlurmClusterReconciler) setUpConditions(ctx context.Context, cluster *slurmv1.SlurmCluster) error {
	return utils.ExecuteMultiStep(ctx,
		"Setting up conditions",
		utils.MultiStepExecutionStrategyCollectErrors,
		utils.MultiStepExecutionStep{
			Name: "Common resources",
			Func: func(stepCtx context.Context) error {
				return r.patchStatus(stepCtx, cluster, func(status *slurmv1.SlurmClusterStatus) {
					status.SetCondition(metav1.Condition{
						Type:    slurmv1.ConditionClusterCommonAvailable,
						Status:  metav1.ConditionUnknown,
						Reason:  "Reconciling",
						Message: "Reconciling Slurm common resources",
					})
				})
			},
		},
		utils.MultiStepExecutionStep{
			Name: "Controllers",
			Func: func(stepCtx context.Context) error {
				return r.patchStatus(stepCtx, cluster, func(status *slurmv1.SlurmClusterStatus) {
					status.SetCondition(metav1.Condition{
						Type:    slurmv1.ConditionClusterControllersAvailable,
						Status:  metav1.ConditionUnknown,
						Reason:  "Reconciling",
						Message: "Reconciling Slurm Controllers",
					})
				})
			},
		},
		utils.MultiStepExecutionStep{
			Name: "Workers",
			Func: func(stepCtx context.Context) error {
				return r.patchStatus(stepCtx, cluster, func(status *slurmv1.SlurmClusterStatus) {
					status.SetCondition(metav1.Condition{
						Type:    slurmv1.ConditionClusterWorkersAvailable,
						Status:  metav1.ConditionUnknown,
						Reason:  "Reconciling",
						Message: "Reconciling Slurm Workers",
					})
				})
			},
		},
		utils.MultiStepExecutionStep{
			Name: "Login",
			Func: func(stepCtx context.Context) error {
				return r.patchStatus(stepCtx, cluster, func(status *slurmv1.SlurmClusterStatus) {
					status.SetCondition(metav1.Condition{
						Type:    slurmv1.ConditionClusterLoginAvailable,
						Status:  metav1.ConditionUnknown,
						Reason:  "Reconciling",
						Message: "Reconciling Slurm Login",
					})
				})
			},
		},
		utils.MultiStepExecutionStep{
			Name: "Accounting",
			Func: func(stepCtx context.Context) error {
				return r.patchStatus(stepCtx, cluster, func(status *slurmv1.SlurmClusterStatus) {
					status.SetCondition(metav1.Condition{
						Type:    slurmv1.ConditionClusterAccountingAvailable,
						Status:  metav1.ConditionUnknown,
						Reason:  "Reconciling",
						Message: "Reconciling Slurm Accounting",
					})
				})
			},
		},
	)
}

func (r *SlurmClusterReconciler) runWithPhase(ctx context.Context, cluster *slurmv1.SlurmCluster, phase *string, do func() (ctrl.Result, error)) (ctrl.Result, error) {
	if err := r.patchStatus(ctx, cluster, func(status *slurmv1.SlurmClusterStatus) {
		status.Phase = phase
	}); err != nil {
		return ctrl.Result{}, err
	}
	return do()
}

func (r *SlurmClusterReconciler) patchStatus(ctx context.Context, cluster *slurmv1.SlurmCluster, patcher statusPatcher) error {
	patch := client.MergeFrom(cluster.DeepCopy())
	patcher(&cluster.Status)

	if err := r.Status().Patch(ctx, cluster, patch); err != nil {
		log.FromContext(ctx).Error(err, "Failed to patch Slurm cluster status")
		return fmt.Errorf("patching cluster status: %w", err)
	}

	return nil
}

type statusPatcher func(status *slurmv1.SlurmClusterStatus)

const (
	podTemplateField                             = ".spec.slurmNodes.exporter.exporter.podTemplateNameRef"
	supervisordConfigMapField                    = ".spec.slurmNodes.worker.supervisordConfigMapRefName"
	sshdLoginConfigMapField                      = ".spec.slurmNodes.login.sshdConfigMapRefName"
	sshdWorkerConfigMapField                     = ".spec.slurmNodes.worker.sshdConfigMapRefName"
	accountingExternalDBPasswordSecretKeyField   = ".spec.slurmNodes.accounting.externalDB.passwordSecretKeyRef.Name"
	accountingExternalDBTLSServerCASecretField   = ".spec.slurmNodes.accounting.externalDB.tls.serverCASecretRef"
	accountingExternalDBTLSClientCertSecretField = ".spec.slurmNodes.accounting.externalDB.tls.clientCertSecretRef"
)

func (r *SlurmClusterReconciler) SetupWithManager(mgr ctrl.Manager, maxConcurrency int, cacheSyncTimeout time.Duration) error {
	if err := r.setupConfigMapIndexer(mgr); err != nil {
		return err
	}
	if err := r.setupSecretIndexer(mgr); err != nil {
		return err
	}

	saPredicate := r.createServiceAccountPredicate()

	controllerBuilder := ctrl.NewControllerManagedBy(mgr).
		Named("cluster").
		For(&slurmv1.SlurmCluster{}, builder.WithPredicates(predicate.GenerationChangedPredicate{}))

	controllerBuilder.Watches(
		&corev1.PodTemplate{},
		handler.EnqueueRequestsFromMapFunc(r.findObjectsForPodTemplate),
		builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
	)

	controllerBuilder.Watches(
		&corev1.ConfigMap{},
		handler.EnqueueRequestsFromMapFunc(r.findObjectsForConfigMap),
		builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
	)

	controllerBuilder.Watches(
		&corev1.Secret{},
		handler.EnqueueRequestsFromMapFunc(r.findObjectsForSecret),
		builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
	)

	resourceChecks := r.createResourceChecks(saPredicate)

	for _, resourceCheck := range resourceChecks {
		if resourceCheck.Check {
			for _, obj := range resourceCheck.Objects {
				controllerBuilder.Owns(obj, builder.WithPredicates(resourceCheck.Predicate))
			}
		}
	}

	controllerBuilder.WithOptions(controllerconfig.ControllerOptions(maxConcurrency, cacheSyncTimeout))

	return controllerBuilder.Complete(r)
}

func (r *SlurmClusterReconciler) setupConfigMapIndexer(mgr ctrl.Manager) error {
	indexers := map[string]func(*slurmv1.SlurmCluster) string{
		supervisordConfigMapField: func(sc *slurmv1.SlurmCluster) string {
			return sc.Spec.SlurmNodes.Worker.SupervisordConfigMapRefName
		},
		sshdLoginConfigMapField: func(sc *slurmv1.SlurmCluster) string {
			return sc.Spec.SlurmNodes.Login.SSHDConfigMapRefName
		},
		sshdWorkerConfigMapField: func(sc *slurmv1.SlurmCluster) string {
			return sc.Spec.SlurmNodes.Worker.SSHDConfigMapRefName
		},
	}

	for field, extractFunc := range indexers {
		err := mgr.GetFieldIndexer().IndexField(context.Background(), &slurmv1.SlurmCluster{}, field, func(rawObj client.Object) []string {
			slurmCluster := rawObj.(*slurmv1.SlurmCluster)
			value := extractFunc(slurmCluster)
			if value == "" {
				return nil
			}
			return []string{value}
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *SlurmClusterReconciler) setupSecretIndexer(mgr ctrl.Manager) error {
	indexers := map[string]func(*slurmv1.SlurmCluster) string{
		accountingExternalDBPasswordSecretKeyField: func(sc *slurmv1.SlurmCluster) string {
			return sc.Spec.SlurmNodes.Accounting.ExternalDB.PasswordSecretKeyRef.Name
		},
		accountingExternalDBTLSServerCASecretField: func(sc *slurmv1.SlurmCluster) string {
			return sc.Spec.SlurmNodes.Accounting.ExternalDB.TLS.ServerCASecretRef
		},
		accountingExternalDBTLSClientCertSecretField: func(sc *slurmv1.SlurmCluster) string {
			return sc.Spec.SlurmNodes.Accounting.ExternalDB.TLS.ClientCertSecretRef
		},
	}

	for field, extractFunc := range indexers {
		err := mgr.GetFieldIndexer().IndexField(context.Background(), &slurmv1.SlurmCluster{}, field, func(rawObj client.Object) []string {
			slurmCluster := rawObj.(*slurmv1.SlurmCluster)
			value := extractFunc(slurmCluster)
			if value == "" {
				return nil
			}
			return []string{value}
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (r *SlurmClusterReconciler) createServiceAccountPredicate() predicate.Funcs {
	return predicate.Funcs{
		DeleteFunc: func(e event.DeleteEvent) bool {
			if sa, ok := e.Object.(*corev1.ServiceAccount); ok {
				return sa.GetDeletionTimestamp() != nil
			}
			return false
		},
		CreateFunc:  func(e event.CreateEvent) bool { return false },
		UpdateFunc:  func(e event.UpdateEvent) bool { return false },
		GenericFunc: func(e event.GenericEvent) bool { return false },
	}
}

/*
Because we have already created an index on the `podTemplate` reference field, this mapping function is quite straightforward.
We first need to list out all `SlurmCluster` that use `podTemplate` given in the mapping function.
This is done by merely submitting a List request using our indexed field as the field selector.

When the list of `SlurmCluster` that reference the `podTemplate` is found,
we just need to loop through the list and create a reconcile request for each one.
If an error occurs fetching the list, or no `SlurmCluster` are found, then no reconcile requests will be returned.
*/
func (r *SlurmClusterReconciler) findObjectsForPodTemplate(ctx context.Context, podTemplate client.Object) []reconcile.Request {
	attachedSlurmClusters := &slurmv1.SlurmClusterList{}
	listOps := &client.ListOptions{
		FieldSelector: fields.OneTermEqualSelector(podTemplateField, podTemplate.GetName()),
		Namespace:     podTemplate.GetNamespace(),
	}
	err := r.List(ctx, attachedSlurmClusters, listOps)
	if err != nil {
		return []reconcile.Request{}
	}

	requests := make([]reconcile.Request, len(attachedSlurmClusters.Items))
	for i, item := range attachedSlurmClusters.Items {
		requests[i] = reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      item.GetName(),
				Namespace: item.GetNamespace(),
			},
		}
	}
	return requests
}

func (r *SlurmClusterReconciler) findObjectsForConfigMap(
	ctx context.Context,
	configmap client.Object,
) []reconcile.Request {
	configMap, ok := configmap.(*corev1.ConfigMap)
	if !ok {
		return nil
	}
	attachedSlurmClusters := &slurmv1.SlurmClusterList{}
	matchingFields := []string{
		supervisordConfigMapField,
		sshdLoginConfigMapField,
		sshdWorkerConfigMapField,
	}

	var requests []reconcile.Request

	for _, field := range matchingFields {
		listOpts := []client.ListOption{
			client.MatchingFields{field: configMap.Name},
			client.InNamespace(configMap.Namespace),
		}
		if err := r.Client.List(ctx, attachedSlurmClusters, listOpts...); err != nil {
			continue
		}
		for _, slurmCluster := range attachedSlurmClusters.Items {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: slurmCluster.Namespace,
					Name:      slurmCluster.Name,
				},
			})
		}
	}

	return requests
}

func (r *SlurmClusterReconciler) findObjectsForSecret(
	ctx context.Context,
	rawSecret client.Object,
) []reconcile.Request {
	secret, ok := rawSecret.(*corev1.Secret)
	if !ok {
		return nil
	}
	attachedSlurmClusters := &slurmv1.SlurmClusterList{}
	matchingFields := []string{
		accountingExternalDBPasswordSecretKeyField,
		accountingExternalDBTLSServerCASecretField,
		accountingExternalDBTLSClientCertSecretField,
	}

	var requests []reconcile.Request

	for _, field := range matchingFields {
		listOpts := []client.ListOption{
			client.MatchingFields{field: secret.Name},
			client.InNamespace(secret.Namespace),
		}
		if err := r.Client.List(ctx, attachedSlurmClusters, listOpts...); err != nil {
			continue
		}
		for _, slurmCluster := range attachedSlurmClusters.Items {
			requests = append(requests, reconcile.Request{
				NamespacedName: types.NamespacedName{
					Namespace: slurmCluster.Namespace,
					Name:      slurmCluster.Name,
				},
			})
		}
	}

	return requests
}

type ResourceCheck struct {
	Check     bool
	Objects   []client.Object
	Predicate predicate.Predicate
}

func (r *SlurmClusterReconciler) createResourceChecks(saPredicate predicate.Funcs) []ResourceCheck {
	return []ResourceCheck{
		{
			Check: check.ForceTrue,
			Objects: []client.Object{
				&corev1.Service{},
				&appsv1.StatefulSet{},
				&appsv1.Deployment{},
				&corev1.PersistentVolumeClaim{},
				&batchv1.Job{},
				&batchv1.CronJob{},
				&rbacv1.Role{},
				&rbacv1.RoleBinding{},
				&corev1.ConfigMap{},
				&corev1.Secret{},
				&kruisev1b1.StatefulSet{},
				&slurmv1alpha1.JailedConfig{},
			},
			Predicate: predicate.GenerationChangedPredicate{},
		},
		{
			Check: check.ForceTrue,
			Objects: []client.Object{
				&corev1.ServiceAccount{},
			},
			Predicate: saPredicate,
		},
		{
			Check: check.IsPrometheusOperatorCRDInstalled,
			Objects: []client.Object{
				&prometheusv1.PodMonitor{},
			},
			Predicate: predicate.GenerationChangedPredicate{},
		},
		{
			Check: check.IsMariaDbOperatorCRDInstalled,
			Objects: []client.Object{
				&mariadbv1alpha1.MariaDB{},
				&mariadbv1alpha1.Grant{},
			},
			Predicate: predicate.GenerationChangedPredicate{},
		},
		{
			Check: check.IsAppArmorOperatorCRDInstalled,
			Objects: []client.Object{
				&apparmor.AppArmorProfile{},
			},
			Predicate: predicate.GenerationChangedPredicate{},
		},
	}
}
