package clustercontroller

import (
	"context"
	errorsStd "errors"
	"os"
	"sync"
	"time"

	"github.com/pkg/errors"
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
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/check"
	"nebius.ai/slurm-operator/internal/controller/reconciler"
	"nebius.ai/slurm-operator/internal/controller/state"
	"nebius.ai/slurm-operator/internal/logfield"
	"nebius.ai/slurm-operator/internal/utils"
	"nebius.ai/slurm-operator/internal/values"

	mariadv1alpha1 "github.com/mariadb-operator/mariadb-operator/api/v1alpha1"
	otelv1beta1 "github.com/open-telemetry/opentelemetry-operator/apis/v1beta1"
	prometheusv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
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
//+kubebuilder:rbac:groups=opentelemetry.io,resources=opentelemetrycollectors,verbs=get;list;watch;update;patch;delete;create
//+kubebuilder:rbac:groups=monitoring.coreos.com,resources=podmonitors,verbs=get;list;watch;update;patch;delete;create
//+kubebuilder:rbac:groups=core,resources=podtemplates,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=serviceaccounts,verbs=get;list;watch;update;patch;delete;create
//+kubebuilder:rbac:groups=k8s.mariadb.com,resources=mariadbs,verbs=get;list;watch;update;patch;delete;create
//+kubebuilder:rbac:groups=k8s.mariadb.com,resources=grants,verbs=get;list;watch;update;patch;delete;create

// SlurmClusterReconciler reconciles a SlurmCluster object
type SlurmClusterReconciler struct {
	*reconciler.Reconciler

	WatchNamespaces WatchNamespaces

	ConfigMap      *reconciler.ConfigMapReconciler
	Secret         *reconciler.SecretReconciler
	CronJob        *reconciler.CronJobReconciler
	Job            *reconciler.JobReconciler
	Service        *reconciler.ServiceReconciler
	StatefulSet    *reconciler.StatefulSetReconciler
	ServiceAccount *reconciler.ServiceAccountReconciler
	Role           *reconciler.RoleReconciler
	RoleBinding    *reconciler.RoleBindingReconciler
	Otel           *reconciler.OtelReconciler
	PodMonitor     *reconciler.PodMonitorReconciler
	Deployment     *reconciler.DeploymentReconciler
	MariaDb        *reconciler.MariaDbReconciler
	MariaDbGrant   *reconciler.MariaDbGrantReconciler
}

func NewSlurmClusterReconciler(client client.Client, scheme *runtime.Scheme, recorder record.EventRecorder) *SlurmClusterReconciler {
	r := reconciler.NewReconciler(client, scheme, recorder)
	watchNamespacesEnv := os.Getenv("SLURM_OPERATOR_WATCH_NAMESPACES")
	return &SlurmClusterReconciler{
		Reconciler:      r,
		WatchNamespaces: NewWatchNamespaces(watchNamespacesEnv),
		ConfigMap:       reconciler.NewConfigMapReconciler(r),
		Secret:          reconciler.NewSecretReconciler(r),
		CronJob:         reconciler.NewCronJobReconciler(r),
		Job:             reconciler.NewJobReconciler(r),
		Service:         reconciler.NewServiceReconciler(r),
		StatefulSet:     reconciler.NewStatefulSetReconciler(r),
		ServiceAccount:  reconciler.NewServiceAccountReconciler(r),
		Role:            reconciler.NewRoleReconciler(r),
		RoleBinding:     reconciler.NewRoleBindingReconciler(r),
		Otel:            reconciler.NewOtelReconciler(r),
		PodMonitor:      reconciler.NewPodMonitorReconciler(r),
		Deployment:      reconciler.NewDeploymentReconciler(r),
		MariaDb:         reconciler.NewMariaDbReconciler(r),
		MariaDbGrant:    reconciler.NewMariaDbGrantReconciler(r),
	}
}

// Reconcile implements the reconciling logic for the Slurm Cluster.
// The reconciling cycle is actually implemented in the auxiliary 'reconcile' method
func (r *SlurmClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithValues(
		logfield.Namespace, req.Namespace,
		logfield.ClusterName, req.Name,
	)
	log.IntoContext(ctx, logger)

	// If the namespace isn't watched, we have nothing to do
	if !r.WatchNamespaces.IsWatched(req.NamespacedName.Namespace) {
		return ctrl.Result{}, nil
	}

	slurmCluster := &slurmv1.SlurmCluster{}
	err := r.Get(ctx, req.NamespacedName, slurmCluster)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("SlurmCluster resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		logger.Error(err, "Failed to get SlurmCluster")
		return ctrl.Result{Requeue: true}, errors.Wrap(err, "getting SlurmCluster")
	}

	// If cluster marked for deletion, we have nothing to do
	if slurmCluster.GetDeletionTimestamp() != nil {
		return ctrl.Result{}, nil
	}

	result, err := r.reconcile(ctx, slurmCluster)
	if err != nil {
		logger.Error(err, "Failed to reconcile SlurmCluster")
		result = ctrl.Result{}
		err = errors.Wrap(err, "reconciling SlurmCluster")
	}

	statusErr := retry.RetryOnConflict(retry.DefaultRetry, func() error {
		cluster := &slurmv1.SlurmCluster{}
		innerErr := r.Get(ctx, req.NamespacedName, cluster)
		if innerErr != nil {
			if apierrors.IsNotFound(innerErr) {
				logger.Info("SlurmCluster resource not found. Ignoring since object must be deleted")
				return nil
			}
			// Error reading the object - requeue the request.
			logger.Error(innerErr, "Failed to get SlurmCluster")
			return errors.Wrap(innerErr, "getting SlurmCluster")
		}

		return r.Status().Update(ctx, cluster)
	})
	if statusErr != nil {
		logger.Error(statusErr, "Failed to update SlurmCluster status")
		result = ctrl.Result{}
		err = errors.Wrap(statusErr, "updating SlurmCluster status")
	}

	return result, errorsStd.Join(err, statusErr)
}

func (r *SlurmClusterReconciler) reconcile(ctx context.Context, cluster *slurmv1.SlurmCluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	{
		kind := cluster.GetObjectKind()
		key := client.ObjectKeyFromObject(cluster)
		if state.ReconciliationState.Present(kind, key) {
			logger.Info("Reconciliation skipped, as object is already present in reconciliation state",
				"kind", kind.GroupVersionKind().String(),
				"key", key.String(),
			)
			return ctrl.Result{}, nil
		}

		state.ReconciliationState.Set(kind, key)
		logger.Info("Reconciliation state set for object",
			"kind", kind.GroupVersionKind().String(),
			"key", key.String(),
		)

		defer func() {
			state.ReconciliationState.Remove(kind, key)
			logger.Info("Reconciliation state removed for object",
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
			if err = r.ReconcilePopulateJail(ctx, clusterValues, cluster); err != nil {
				return ctrl.Result{}, err
			}
			if err = r.ReconcileAccounting(ctx, cluster, clusterValues); err != nil {
				return ctrl.Result{}, err
			}
			if err = r.ReconcileCommon(ctx, cluster, clusterValues); err != nil {
				return ctrl.Result{}, err
			}
			if err = r.ReconcileNCCLBenchmark(ctx, cluster, clusterValues); err != nil {
				return ctrl.Result{}, err
			}
			if err = r.ReconcileControllers(ctx, cluster, clusterValues); err != nil {
				return ctrl.Result{}, err
			}
			if err = r.ReconcileWorkers(ctx, cluster, clusterValues); err != nil {
				return ctrl.Result{}, err
			}
			if clusterValues.NodeLogin.Size > 0 {
				if err = r.ReconcileLogin(ctx, cluster, clusterValues); err != nil {
					return ctrl.Result{}, err
				}
			}
			if err = r.ReconcileExporter(ctx, cluster, clusterValues); err != nil {
				return ctrl.Result{}, err
			}

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
			if err = r.patchStatus(ctx, cluster, func(status *slurmv1.SlurmClusterStatus) {
				status.SetCondition(metav1.Condition{
					Type:   slurmv1.ConditionClusterCommonAvailable,
					Status: metav1.ConditionTrue, Reason: "Available",
					Message: "Slurm common components are available",
				})
			}); err != nil {
				return ctrl.Result{}, err
			}

			// Controllers
			if res, err := r.ValidateControllers(ctx, cluster, clusterValues); err != nil {
				logger.Error(err, "Failed to validate Slurm controllers")
				return ctrl.Result{}, errors.Wrap(err, "validating Slurm controllers")
			} else if res.Requeue {
				return res, nil
			}

			// Workers
			if res, err := r.ValidateWorkers(ctx, cluster, clusterValues); err != nil {
				logger.Error(err, "Failed to validate Slurm workers")
				return ctrl.Result{}, errors.Wrap(err, "validating Slurm workers")
			} else if res.Requeue {
				return res, nil
			}

			// Login
			if clusterValues.NodeLogin.Size > 0 {
				if res, err := r.ValidateLogin(ctx, cluster, clusterValues); err != nil {
					logger.Error(err, "Failed to validate Slurm login")
					return ctrl.Result{}, errors.Wrap(err, "validating Slurm login")
				} else if res.Requeue {
					return res, nil
				}
			} else {
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
			if clusterValues.NodeAccounting.Enabled {
				if res, err := r.ValidateAccounting(ctx, cluster, clusterValues); err != nil {
					logger.Error(err, "Failed to validate Slurm accounting")
					return ctrl.Result{}, errors.Wrap(err, "validating Slurm accounting")
				} else if res.Requeue {
					return res, nil
				}
			} else {
				if err = r.patchStatus(ctx, cluster, func(status *slurmv1.SlurmClusterStatus) {
					status.SetCondition(metav1.Condition{
						Type:   slurmv1.ConditionClusterAccountingAvailable,
						Status: metav1.ConditionFalse, Reason: "NotAvailable",
						Message: "Slurm accounting is disabled",
					})
				}); err != nil {
					return ctrl.Result{}, err
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
		return errors.Wrap(err, "patching cluster status")
	}

	return nil
}

type statusPatcher func(status *slurmv1.SlurmClusterStatus)

const (
	podTemplateField = ".spec.metrics.podTemplateNameRef"
)

func (r *SlurmClusterReconciler) SetupWithManager(mgr ctrl.Manager, maxConcurrency int, cacheSyncTimeout time.Duration) error {
	if err := r.setupPodTemplateIndexer(mgr); err != nil {
		return err
	}

	saPredicate := r.createServiceAccountPredicate()

	controllerBuilder := ctrl.NewControllerManagedBy(mgr).
		For(&slurmv1.SlurmCluster{}, builder.WithPredicates(predicate.GenerationChangedPredicate{}))

	controllerBuilder.Watches(
		&corev1.PodTemplate{},
		handler.EnqueueRequestsFromMapFunc(r.findObjectsForPodTemplate),
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

	controllerBuilder.WithOptions(getDefaultOptions(maxConcurrency, cacheSyncTimeout))

	return controllerBuilder.Complete(r)
}

func (r *SlurmClusterReconciler) setupPodTemplateIndexer(mgr ctrl.Manager) error {
	return mgr.GetFieldIndexer().IndexField(context.Background(), &slurmv1.SlurmCluster{}, podTemplateField, func(rawObj client.Object) []string {
		slurmCluster := rawObj.(*slurmv1.SlurmCluster)
		if slurmCluster.Spec.Telemetry == nil || slurmCluster.Spec.Telemetry.OpenTelemetryCollector == nil || slurmCluster.Spec.Telemetry.OpenTelemetryCollector.PodTemplateNameRef == nil {
			return nil
		}
		return []string{*slurmCluster.Spec.Telemetry.OpenTelemetryCollector.PodTemplateNameRef}
	})
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
Because we have already created an index on the `podTemplate` reference field, this mapping function is quite straight forward.
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

type ResourceCheck struct {
	Check     bool
	Objects   []client.Object
	Predicate predicate.Predicate
}

func (r *SlurmClusterReconciler) createResourceChecks(saPredicate predicate.Funcs) []ResourceCheck {
	return []ResourceCheck{
		{
			Check: true,
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
			},
			Predicate: predicate.GenerationChangedPredicate{},
		},
		{
			Check: true,
			Objects: []client.Object{
				&corev1.ServiceAccount{},
			},
			Predicate: saPredicate,
		},
		{
			Check: check.IsOpenTelemetryCollectorCRDInstalled,
			Objects: []client.Object{
				&otelv1beta1.OpenTelemetryCollector{},
			},
			Predicate: predicate.GenerationChangedPredicate{},
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
				&mariadv1alpha1.MariaDB{},
				&mariadv1alpha1.Grant{},
			},
			Predicate: predicate.GenerationChangedPredicate{},
		},
	}
}

var (
	optionsInit    sync.Once
	defaultOptions *controller.Options
)

func getDefaultOptions(maxConcurrency int, cacheSyncTimeout time.Duration) controller.Options {
	optionsInit.Do(func() {
		defaultOptions = &controller.Options{
			RateLimiter:             workqueue.NewItemExponentialFailureRateLimiter(2*time.Second, 2*time.Minute),
			CacheSyncTimeout:        cacheSyncTimeout,
			MaxConcurrentReconciles: maxConcurrency,
		}
	})
	return *defaultOptions
}
