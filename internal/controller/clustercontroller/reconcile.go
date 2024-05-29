package clustercontroller

import (
	"context"
	"errors"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/models/slurm"
	"nebius.ai/slurm-operator/internal/reconciler"
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

// SlurmClusterReconciler reconciles a SlurmCluster object
type SlurmClusterReconciler struct {
	reconciler.Reconciler
}

// Reconcile implements the reconciling logic for the Slurm Cluster.
// The reconciling cycle is actually implemented in the auxiliary 'reconcile' method
func (r *SlurmClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	slurmCluster := &slurmv1.SlurmCluster{}
	err := r.Get(ctx, req.NamespacedName, slurmCluster)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("slurmCluster resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		logger.Error(err, "Failed to get slurmCluster")
		return ctrl.Result{}, err
	}

	// If cluster marked for deletion, we have nothing to do
	if slurmCluster.GetDeletionTimestamp() != nil {
		return ctrl.Result{}, nil
	}

	result, err := r.reconcile(ctx, slurmCluster)
	if err != nil {
		logger.Error(err, "Failed to reconcile SlurmCluster")
		result = ctrl.Result{}
	}

	statusErr := r.Status().Update(ctx, slurmCluster)
	if statusErr != nil {
		logger.Error(statusErr, "Failed to update SlurmCluster status")
		result = ctrl.Result{}
	}

	return result, errors.Join(err, statusErr)
}

func (r *SlurmClusterReconciler) reconcile(ctx context.Context, slurmCluster *slurmv1.SlurmCluster) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Starting reconciliation of Slurm Cluster",
		"SlurmCluster.Namespace", slurmCluster.Namespace, "SlurmCluster.Name", slurmCluster.Name)

	slurmClusterValues, err := smodels.BuildClusterValuesFrom(slurmCluster)
	if err != nil {
		return ctrl.Result{}, err
	}

	meta.SetStatusCondition(
		&slurmCluster.Status.Conditions,
		metav1.Condition{
			Type:    slurmv1.ConditionClusterControllersAvailable,
			Status:  metav1.ConditionUnknown,
			Reason:  "Reconciling",
			Message: "Reconciling Slurm Controllers",
		})

	// TODO workers reconcilation
	//meta.SetStatusCondition(
	//	&slurmCluster.Status.Conditions,
	//	metav1.Condition{
	//		Type:    slurmv1.ConditionClusterWorkersAvailable,
	//		Status:  metav1.ConditionUnknown,
	//		Reason:  "Reconciling",
	//		Message: "Reconciling Slurm Workers",
	//	})

	initialPhase := slurmv1.PhaseClusterReconciling
	slurmCluster.Status.Phase = &initialPhase

	// Reconcile Controllers
	if res, err := r.DeployControllers(ctx, slurmClusterValues, slurmCluster); err != nil {
		return res, err
	}
	if res, err := r.UpdateControllers(ctx, slurmClusterValues, slurmCluster); err != nil {
		return res, err
	}

	// Reconcile Workers
	// TODO workers reconcilation
	//if res, err := r.DeployWorkers(ctx, slurmClusterValues, slurmCluster); err != nil {
	//	return res, err
	//}
	//if res, err := r.UpdateWorkers(ctx, slurmClusterValues, slurmCluster); err != nil {
	//	return res, err
	//}

	notAvailablePhase := slurmv1.PhaseClusterNotAvailable
	slurmCluster.Status.Phase = &notAvailablePhase

	// Validate Controllers
	if res, err := r.ValidateControllers(ctx, slurmClusterValues, slurmCluster); err != nil {
		return res, err
	} else if res.Requeue {
		return res, err
	}

	// Validate Workers
	// TODO workers validation
	//if res, err := r.ValidateWorkers(ctx, slurmClusterValues, slurmCluster); err != nil {
	//	return res, err
	//} else if res.Requeue {
	//	return res, err
	//}

	availablePhase := slurmv1.PhaseClusterAvailable
	slurmCluster.Status.Phase = &availablePhase

	logger.Info("Finished reconciliation of Slurm Cluster",
		"SlurmCluster.Namespace", slurmCluster.Namespace, "SlurmCluster.Name", slurmCluster.Name)

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SlurmClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&slurmv1.SlurmCluster{}).
		Owns(&corev1.Service{}).
		Owns(&appsv1.StatefulSet{}).
		Owns(&corev1.PersistentVolumeClaim{}).
		Complete(r)
}
