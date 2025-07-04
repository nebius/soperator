package soperatorchecks

import (
	"context"
	"fmt"
	"time"

	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	maintenance "nebius.ai/slurm-operator/internal/check"
	"nebius.ai/slurm-operator/internal/logfield"
	render "nebius.ai/slurm-operator/internal/render/soperatorchecks"
	"nebius.ai/slurm-operator/internal/utils"

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

// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=activechecks,verbs=get;list;watch
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

	cluster := &slurmv1.SlurmCluster{}
	clusterNN := types.NamespacedName{
		Namespace: req.Namespace,
		Name:      check.Spec.SlurmClusterRefName,
	}
	err = r.Get(ctx, clusterNN, cluster)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, fmt.Errorf("SlurmCluster resource not found: %w", err)
		}

		logger.Error(err, "Failed to get SlurmCluster")
		return ctrl.Result{}, fmt.Errorf("getting SlurmCluster: %w", err)
	}

	if maintenance.IsMaintenanceActive(cluster.Spec.Maintenance) {
		logger.Info(fmt.Sprintf(
			"Slurm cluster maintenance status is %s, skip ActiveCheck ServiceAccount reconcile",
			*cluster.Spec.Maintenance,
		))
		return ctrl.Result{}, nil
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

					if err := r.ServiceAccount.Reconcile(stepCtx, cluster, desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return fmt.Errorf("reconciling active check ServiceAccount: %w", err)
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

					if err := r.Role.Reconcile(stepCtx, cluster, desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return fmt.Errorf("reconciling active check Role: %w", err)
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

					if err := r.RoleBinding.Reconcile(stepCtx, cluster, desired); err != nil {
						stepLogger.Error(err, "Failed to reconcile")
						return fmt.Errorf("reconciling active check RoleBinding: %w", err)
					}

					stepLogger.V(1).Info("Reconciled")

					return nil
				},
			},
		)
	}

	if err := reconcileServiceAccountImpl(); err != nil {
		logger.Error(err, "Failed to reconcile ServiceAccount")
		return ctrl.Result{}, fmt.Errorf("reconciling ServiceAccount: %w", err)
	}

	logger.Info("Reconciled ServiceAccount")
	return ctrl.Result{}, nil
}
