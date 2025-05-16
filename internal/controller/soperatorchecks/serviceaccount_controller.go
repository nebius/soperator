package soperatorchecks

import (
	"context"
	"time"

	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/logfield"
	"nebius.ai/slurm-operator/internal/naming"
	render "nebius.ai/slurm-operator/internal/render/soperatorchecks"
	"nebius.ai/slurm-operator/internal/utils"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/controller/reconciler"
	"nebius.ai/slurm-operator/internal/controllerconfig"
)

var (
	SlurmChecksServiceAccountControllerName = "soperatorchecks.serviceaccount"
)

type ServiceAccountReconciler struct {
	*reconciler.Reconciler
	reconcileTimeout time.Duration

	ServiceAccount *reconciler.ServiceAccountReconciler
	Role           *reconciler.RoleReconciler
	RoleBinding    *reconciler.RoleBindingReconciler
}

func NewServiceAccountController(
	client client.Client,
	scheme *runtime.Scheme,
	recorder record.EventRecorder,
	reconcileTimeout time.Duration,
) *ServiceAccountReconciler {
	r := reconciler.NewReconciler(client, scheme, recorder)
	serviceAccountReconciler := reconciler.NewServiceAccountReconciler(r)
	roleReconciler := reconciler.NewRoleReconciler(r)
	roleBindingReconciler := reconciler.NewRoleBindingReconciler(r)

	return &ServiceAccountReconciler{
		Reconciler:       r,
		reconcileTimeout: reconcileTimeout,
		ServiceAccount:   serviceAccountReconciler,
		Role:             roleReconciler,
		RoleBinding:      roleBindingReconciler,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *ServiceAccountReconciler) SetupWithManager(
	mgr ctrl.Manager,
	maxConcurrency int,
	cacheSyncTimeout time.Duration,
) error {
	return ctrl.NewControllerManagedBy(mgr).
		Named(SlurmChecksServiceAccountControllerName).
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
						return ac.GetDeletionTimestamp() != nil
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

// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=activechecks,verbs=get;list;watch;update;patch
// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=activechecks/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=activechecks/finalizers,verbs=update
// +kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=roles;rolebindings,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=serviceaccounts,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=pods,verbs=get
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;patch
// +kubebuilder:rbac:groups="",resources=events,verbs=create;patch

// Reconcile reconciles service account resources active checks
func (r *ServiceAccountReconciler) Reconcile(
	ctx context.Context,
	req ctrl.Request,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName("ServiceAccountController.reconcile")

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

	if !check.DeletionTimestamp.IsZero() {
		if controllerutil.ContainsFinalizer(check, consts.ActiveCheckServiceAccountFinalizer) {
			return r.reconcileDelete(ctx, check)
		}

		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(check, consts.ActiveCheckServiceAccountFinalizer) {
		controllerutil.AddFinalizer(check, consts.ActiveCheckServiceAccountFinalizer)
		if err := r.Update(ctx, check); err != nil {
			logger.Error(err, "Failed to add finalizer")
			return ctrl.Result{}, err
		}
	}

	cluster := &slurmv1.SlurmCluster{}
	clusterNN := types.NamespacedName{
		Namespace: req.Namespace,
		Name:      check.Spec.SlurmClusterRefName,
	}
	err = r.Get(ctx, clusterNN, cluster)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, errors.Wrap(err, "SlurmCluster resource not found")
		}

		logger.Error(err, "Failed to get SlurmCluster")
		return ctrl.Result{}, errors.Wrap(err, "getting SlurmCluster")
	}

	reconcileServiceAccountImpl := func() error {
		return utils.ExecuteMultiStep(ctx,
			"Reconciliation of slurm active check service account",
			utils.MultiStepExecutionStrategyFailAtFirstError,

			utils.MultiStepExecutionStep{
				Name: "Slurm active check ServiceAccount",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")

					desired := render.RenderServiceAccount(clusterNN.Namespace, clusterNN.Name)
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.V(1).Info("Rendered")

					if err := r.ServiceAccount.Reconcile(stepCtx, cluster, &desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling active check ServiceAccount")
					}
					stepLogger.V(1).Info("Reconciled")

					return nil
				},
			},

			utils.MultiStepExecutionStep{
				Name: "Slurm active check Role",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")

					desired := render.RenderRole(clusterNN.Namespace, clusterNN.Name)
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.V(1).Info("Rendered")

					if err := r.Role.Reconcile(stepCtx, cluster, &desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling active check Role")
					}
					stepLogger.V(1).Info("Reconciled")

					return nil
				},
			},

			utils.MultiStepExecutionStep{
				Name: "Slurm active check RoleBinding",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")

					desired := render.RenderRoleBinding(clusterNN.Namespace, clusterNN.Name)
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.V(1).Info("Rendered")

					if err := r.RoleBinding.Reconcile(stepCtx, cluster, &desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return errors.Wrap(err, "reconciling active check RoleBinding")
					}

					stepLogger.V(1).Info("Reconciled")

					return nil
				},
			},

			utils.MultiStepExecutionStep{
				Name: "Slurm active check status update",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")

					check.Status.ServiceAccountReady = true
					if err = r.Status().Update(ctx, check); err != nil {
						logger.Error(err, "Failed to update status")
						return errors.Wrap(err, "reconciling active check status")
					}

					return nil
				},
			},
		)
	}

	if err := reconcileServiceAccountImpl(); err != nil {
		logger.Error(err, "Failed to reconcile ServiceAccount")
		return ctrl.Result{}, errors.Wrap(err, "reconciling ServiceAccount")
	}

	logger.Info("Reconciled ServiceAccount")
	return ctrl.Result{}, nil
}

func (r *ServiceAccountReconciler) reconcileDelete(ctx context.Context, check *slurmv1alpha1.ActiveCheck) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName("ServiceAccountController.reconcileDelete")

	var checks slurmv1alpha1.ActiveCheckList
	if err := r.List(ctx, &checks, client.InNamespace(check.Namespace)); err != nil {
		logger.Error(err, "Failed to list ActiveChecks")
		return ctrl.Result{}, err
	}

	if len(checks.Items) > 1 {
		logger.Info("More than 1 check left, removing finalizer")

		controllerutil.RemoveFinalizer(check, consts.ActiveCheckServiceAccountFinalizer)
		if err := r.Update(ctx, check); err != nil {
			logger.Error(err, "Failed to remove finalizer")
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, nil
	}

	clusterName := check.Spec.SlurmClusterRefName
	resources := []client.Object{
		&corev1.ServiceAccount{ObjectMeta: objectMeta(naming.BuildServiceAccountActiveCheckName(clusterName), check.Namespace)},
		&rbacv1.Role{ObjectMeta: objectMeta(naming.BuildRoleActiveCheckName(clusterName), check.Namespace)},
		&rbacv1.RoleBinding{ObjectMeta: objectMeta(naming.BuildRoleBindingActiveCheckName(clusterName), check.Namespace)},
	}

	for _, obj := range resources {
		if err := r.Delete(ctx, obj); client.IgnoreNotFound(err) != nil {
			logger.Error(err, "Failed to delete", "name", obj.GetName())
			return ctrl.Result{}, err
		}
		logger.Info("Deleted", "name", obj.GetName())
	}

	controllerutil.RemoveFinalizer(check, consts.ActiveCheckServiceAccountFinalizer)
	if err := r.Update(ctx, check); err != nil {
		logger.Error(err, "Failed to remove finalizer")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}

func objectMeta(name, namespace string) metav1.ObjectMeta {
	return metav1.ObjectMeta{
		Name:      name,
		Namespace: namespace,
	}
}
