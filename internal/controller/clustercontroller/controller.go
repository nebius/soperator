package clustercontroller

import (
	"cmp"
	"context"
	"fmt"
	"strings"
	"time"

	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/logfield"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/render/controller"
	"nebius.ai/slurm-operator/internal/utils"
	"nebius.ai/slurm-operator/internal/values"
)

// ReconcileControllers reconciles all resources necessary for deploying Slurm controllers
func (r SlurmClusterReconciler) ReconcileControllers(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	clusterValues *values.SlurmCluster,
) error {
	logger := log.FromContext(ctx)

	reconcileControllersImpl := func() error {
		return utils.ExecuteMultiStep(ctx,
			"Reconciliation of Slurm Controllers",
			utils.MultiStepExecutionStrategyCollectErrors,

			utils.MultiStepExecutionStep{
				Name: "Slurm Controller Security limits ConfigMap",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")

					desired := common.RenderConfigMapSecurityLimits(consts.ComponentTypeController, clusterValues)
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.V(1).Info("Rendered")

					if err := r.ConfigMap.Reconcile(stepCtx, cluster, &desired); err != nil {
						return fmt.Errorf("reconciling controller security limits configmap: %w", err)
					}
					stepLogger.V(1).Info("Reconciled")

					return nil
				},
			},

			utils.MultiStepExecutionStep{
				Name: "Slurm Controller Service",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")

					replicas := cmp.Or(clusterValues.NodeController.StatefulSet.Replicas, 1)
					createdServices := map[string]bool{}

					for i := int32(0); i < replicas; i++ {
						svcName := fmt.Sprintf("%s-%d", clusterValues.NodeController.StatefulSet.Name, i)
						desired := controller.RenderService(clusterValues.Namespace, clusterValues.Name, svcName, &clusterValues.NodeController,
							map[string]string{
								"statefulset.kubernetes.io/pod-name": fmt.Sprintf("controller-%d", i),
							})
						stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
						stepLogger.V(1).Info("Rendered per-pod service", "service", svcName)
						var controllerNamePtr *string = nil
						if err := r.Service.Reconcile(stepCtx, cluster, &desired, controllerNamePtr); err != nil {
							return fmt.Errorf("reconciling controller Service %s: %w", svcName, err)
						}
						createdServices[svcName] = true
					}

					svcList := &corev1.ServiceList{}
					if err := r.List(stepCtx, svcList, client.InNamespace(clusterValues.Namespace)); err != nil {
						return fmt.Errorf("listing services for cleanup: %w", err)
					}
					prefix := clusterValues.NodeController.StatefulSet.Name + "-"
					for _, svc := range svcList.Items {
						if svc.Namespace != clusterValues.Namespace {
							continue
						}
						if !strings.HasPrefix(svc.Name, prefix) {
							continue
						}
						if _, ok := createdServices[svc.Name]; !ok {
							stepLogger.Info("Deleting excess service", "service", svc.Name)
							if err := r.Service.Delete(stepCtx, &svc, []client.DeleteOption{}...); err != nil {
								return fmt.Errorf("deleting excess service %s: %w", svc.Name, err)
							}
						}
					}

					stepLogger.V(1).Info("Reconciled per-pod services")
					return nil
				},
			},
			utils.MultiStepExecutionStep{
				Name: "Slurm Controller StatefulSet",
				Func: func(stepCtx context.Context) error {
					stepLogger := log.FromContext(stepCtx)
					stepLogger.V(1).Info("Reconciling")

					desired, err := controller.RenderStatefulSet(
						clusterValues.Namespace,
						clusterValues.Name,
						clusterValues.NodeFilters,
						clusterValues.VolumeSources,
						&clusterValues.NodeController,
						clusterValues.SlurmTopologyConfigMapRefName,
					)
					if err != nil {
						stepLogger.Error(err, "Failed to render")
						return fmt.Errorf("rendering controller StatefulSet: %w", err)
					}
					stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
					stepLogger.V(1).Info("Rendered")

					deps, err := r.getControllersStatefulSetDependencies(stepCtx, clusterValues)
					if err != nil {
						return fmt.Errorf("retrieving dependencies for controller StatefulSet: %w", err)
					}
					stepLogger.V(1).Info("Retrieved dependencies")

					if err = r.AdvancedStatefulSet.Reconcile(stepCtx, cluster, &desired, deps...); err != nil {
						return fmt.Errorf("reconciling controller StatefulSet: %w", err)
					}
					stepLogger.V(1).Info("Reconciled")

					return nil
				},
			},
		)
	}

	if err := reconcileControllersImpl(); err != nil {
		return fmt.Errorf("reconciling Slurm Controllers: %w", err)
	}
	logger.Info("Reconciled Slurm Controllers")
	return nil
}

// ValidateControllers checks that Slurm controllers are reconciled with the desired state correctly
func (r SlurmClusterReconciler) ValidateControllers(
	ctx context.Context,
	cluster *slurmv1.SlurmCluster,
	clusterValues *values.SlurmCluster,
) (ctrl.Result, error) {

	existing := &kruisev1b1.StatefulSet{}
	err := r.Get(
		ctx,
		types.NamespacedName{
			Namespace: clusterValues.Namespace,
			Name:      clusterValues.NodeController.StatefulSet.Name,
		},
		existing,
	)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{Requeue: true, RequeueAfter: 5 * time.Second}, nil
		}
		return ctrl.Result{}, fmt.Errorf("getting controller StatefulSet: %w", err)
	}

	targetReplicas := clusterValues.NodeController.StatefulSet.Replicas
	if existing.Spec.Replicas != nil {
		targetReplicas = *existing.Spec.Replicas
	}
	if existing.Status.AvailableReplicas != targetReplicas {
		if err = r.patchStatus(ctx, cluster, func(status *slurmv1.SlurmClusterStatus) {
			status.SetCondition(metav1.Condition{
				Type:   slurmv1.ConditionClusterControllersAvailable,
				Status: metav1.ConditionFalse, Reason: "NotAvailable",
				Message: "Slurm controllers are not available yet",
			})
		}); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true, RequeueAfter: time.Second * 10}, nil
	} else {
		if err = r.patchStatus(ctx, cluster, func(status *slurmv1.SlurmClusterStatus) {
			status.SetCondition(metav1.Condition{
				Type:   slurmv1.ConditionClusterControllersAvailable,
				Status: metav1.ConditionTrue, Reason: "Available",
				Message: "Slurm controllers are available",
			})
		}); err != nil {
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r SlurmClusterReconciler) getControllersStatefulSetDependencies(
	ctx context.Context,
	clusterValues *values.SlurmCluster,
) ([]metav1.Object, error) {
	var res []metav1.Object

	mungeKeySecret := &corev1.Secret{}
	if err := r.Get(
		ctx,
		types.NamespacedName{
			Namespace: clusterValues.Namespace,
			Name:      naming.BuildSecretMungeKeyName(clusterValues.Name),
		},
		mungeKeySecret,
	); err != nil {
		return []metav1.Object{}, err
	}
	res = append(res, mungeKeySecret)

	if clusterValues.NodeAccounting.Enabled {
		slurmdbdSecret := &corev1.Secret{}
		if err := r.Get(
			ctx,
			types.NamespacedName{
				Namespace: clusterValues.Namespace,
				Name:      naming.BuildSecretSlurmdbdConfigsName(clusterValues.Name),
			},
			slurmdbdSecret,
		); err != nil {
			return []metav1.Object{}, err
		}
		res = append(res, slurmdbdSecret)
	}

	return res, nil
}
