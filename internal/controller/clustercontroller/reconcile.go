package clustercontroller

import (
	"context"
	errorsStd "errors"
	"os"

	"github.com/pkg/errors"
	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/controller/reconciler"
	"nebius.ai/slurm-operator/internal/controller/state"
	"nebius.ai/slurm-operator/internal/logfield"
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
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;watch;list
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=jobs/status,verbs=get;update
// +kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=batch,resources=cronjobs/status,verbs=get;update

// SlurmClusterReconciler reconciles a SlurmCluster object
type SlurmClusterReconciler struct {
	*reconciler.Reconciler

	WatchNamespaces WatchNamespaces

	ConfigMap   *reconciler.ConfigMapReconciler
	Secret      *reconciler.SecretReconciler
	CronJob     *reconciler.CronJobReconciler
	Job         *reconciler.JobReconciler
	Service     *reconciler.ServiceReconciler
	StatefulSet *reconciler.StatefulSetReconciler
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

	if state.ReconciliationState.Present(cluster.GetObjectKind(), client.ObjectKeyFromObject(cluster)) {
		return ctrl.Result{}, nil
	}
	state.ReconciliationState.Set(cluster.GetObjectKind(), client.ObjectKeyFromObject(cluster))
	defer func() {
		state.ReconciliationState.Remove(cluster.GetObjectKind(), client.ObjectKeyFromObject(cluster))
	}()

	logger.Info("Starting reconciliation of Slurm Cluster")

	clusterValues, err := values.BuildSlurmClusterFrom(ctx, cluster)
	if err != nil {
		return ctrl.Result{}, err
	}

	r.setUpConditions(cluster)

	// Reconciliation
	res, err := r.runWithPhase(ctx, cluster,
		ptr.To(slurmv1.PhaseClusterReconciling),
		func() (ctrl.Result, error) {
			result, wait, err := r.ReconcilePopulateJail(ctx, clusterValues, cluster)
			if err != nil || wait == true {
				return result, err
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

func (r *SlurmClusterReconciler) setUpConditions(cluster *slurmv1.SlurmCluster) {
	meta.SetStatusCondition(
		&cluster.Status.Conditions,
		metav1.Condition{
			Type:    slurmv1.ConditionClusterCommonAvailable,
			Status:  metav1.ConditionUnknown,
			Reason:  "Reconciling",
			Message: "Reconciling Slurm common resources",
		},
	)
	meta.SetStatusCondition(
		&cluster.Status.Conditions,
		metav1.Condition{
			Type:    slurmv1.ConditionClusterControllersAvailable,
			Status:  metav1.ConditionUnknown,
			Reason:  "Reconciling",
			Message: "Reconciling Slurm Controllers",
		},
	)
	meta.SetStatusCondition(
		&cluster.Status.Conditions,
		metav1.Condition{
			Type:    slurmv1.ConditionClusterWorkersAvailable,
			Status:  metav1.ConditionUnknown,
			Reason:  "Reconciling",
			Message: "Reconciling Slurm Workers",
		},
	)
	meta.SetStatusCondition(
		&cluster.Status.Conditions,
		metav1.Condition{
			Type:    slurmv1.ConditionClusterLoginAvailable,
			Status:  metav1.ConditionUnknown,
			Reason:  "Reconciling",
			Message: "Reconciling Slurm Login",
		},
	)
}

func (r *SlurmClusterReconciler) runWithPhase(ctx context.Context, cluster *slurmv1.SlurmCluster, phase *string, do func() (ctrl.Result, error)) (ctrl.Result, error) {
	patch := client.MergeFrom(cluster.DeepCopy())
	cluster.Status.Phase = phase
	if err := r.Status().Patch(ctx, cluster, patch); err != nil {
		log.FromContext(ctx).Error(err, "Failed to update Slurm cluster status phase")
		return ctrl.Result{}, errors.Wrap(err, "updating cluster status phase")
	}

	return do()
}

// SetupWithManager sets up the controller with the Manager.
func (r *SlurmClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := indexFields(mgr); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&slurmv1.SlurmCluster{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Owns(&corev1.Service{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Owns(&corev1.ConfigMap{}).
		Owns(&batchv1.Job{}).
		Owns(&batchv1.CronJob{}).
		Owns(&corev1.Secret{}).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.mapObjectsToReconcileRequests),
		).
		Complete(r)
}
