package soperatorchecks

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/pkg/errors"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/logfield"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"nebius.ai/slurm-operator/internal/controller/reconciler"
	"nebius.ai/slurm-operator/internal/controllerconfig"
)

var (
	SlurmActiveCheckJobControllerName = "soperatorchecks.activecheckjob"
)

type ActiveCheckJobReconciler struct {
	*reconciler.Reconciler
	reconcileTimeout time.Duration
}

func NewActiveCheckJobController(
	client client.Client,
	scheme *runtime.Scheme,
	recorder record.EventRecorder,
	reconcileTimeout time.Duration,
) *ActiveCheckJobReconciler {
	r := reconciler.NewReconciler(client, scheme, recorder)

	return &ActiveCheckJobReconciler{
		Reconciler:       r,
		reconcileTimeout: reconcileTimeout,
	}
}

func (r *ActiveCheckJobReconciler) SetupWithManager(mgr ctrl.Manager,
	maxConcurrency int, cacheSyncTimeout time.Duration) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&batchv1.Job{}, builder.WithPredicates(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				configMap, ok := e.Object.(*corev1.ConfigMap)
				if !ok {
					return false
				}

				return isValidConfigMap(configMap)
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				configMap, ok := e.ObjectNew.(*corev1.ConfigMap)
				if !ok {
					return false
				}

				return isValidConfigMap(configMap)
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				return false
			},
			GenericFunc: func(e event.GenericEvent) bool {
				configMap, ok := e.Object.(*corev1.ConfigMap)
				if !ok {
					return false
				}

				return isValidConfigMap(configMap)
			},
		})).
		WithOptions(controllerconfig.ControllerOptions(maxConcurrency, cacheSyncTimeout)).
		Complete(r)
}

func isValidConfigMap(cm *corev1.ConfigMap) bool {
	if v, ok := cm.Annotations[consts.LabelComponentKey]; ok {
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

	job := &batchv1.Job{}
	err := r.Get(ctx, req.NamespacedName, job)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("ActiveCheckJob resource not found. Ignoring since object must be deleted.")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get ActiveCheckJob")
		return ctrl.Result{}, err
	}

	activeCheckNamespacedName := job.Annotations[consts.AnnotationActiveCheckKey]
	parts := strings.SplitN(activeCheckNamespacedName, "/", 2)
	if len(parts) != 2 {
		return ctrl.Result{}, fmt.Errorf("invalid input format")
	}
	activeCheckNamespace := parts[0]
	activeCheckName := parts[1]
	check := &slurmv1alpha1.ActiveCheck{}
	err = r.Get(ctx, types.NamespacedName{
		Namespace: activeCheckNamespace,
		Name:      activeCheckName,
	}, check)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("ActiveCheck resource not found. Ignoring since object must be deleted.")
			return ctrl.Result{}, nil
		}
		logger.Error(err, "Failed to get ActiveCheck")
		return ctrl.Result{}, err
	}

	JobStatusKey := fmt.Sprintf("%s/%s", job.Namespace, job.Name)
	check.Status.Jobs[JobStatusKey] = job.Status

	logger = logger.WithValues(logfield.ResourceKV(check)...)
	logger.V(1).Info("Rendered")

	err = r.Status().Update(ctx, check)
	if err != nil {
		logger.Error(err, "Failed to reconcile ActiveCheckJob")
		return ctrl.Result{}, errors.Wrap(err, "reconciling ActiveCheckJob")
	}

	logger.Info("Reconciled ActiveCheckJob")
	return ctrl.Result{}, nil
}
