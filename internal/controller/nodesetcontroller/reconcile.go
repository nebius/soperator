package nodesetcontroller

import (
	"context"
	"fmt"
	"time"

	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/check"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/controller/state"
	"nebius.ai/slurm-operator/internal/logfield"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/render/common"
	"nebius.ai/slurm-operator/internal/render/worker"
	"nebius.ai/slurm-operator/internal/utils"
	"nebius.ai/slurm-operator/internal/utils/resourcegetter"
	"nebius.ai/slurm-operator/internal/values"
)

func (r *NodeSetReconciler) reconcile(ctx context.Context, nodeSet *slurmv1alpha1.NodeSet) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	// region Synchronous reconciliation
	{
		kind := nodeSet.GetObjectKind()
		key := client.ObjectKeyFromObject(nodeSet)
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
	// endregion Synchronous reconciliation

	logger.Info("Starting reconciliation")

	// region Get parental cluster
	var (
		cluster *slurmv1.SlurmCluster
		err     error
	)
	{
		clusterName, hasClusterRef := nodeSet.GetAnnotations()[consts.AnnotationParentalClusterRefName]
		if !hasClusterRef {
			err = fmt.Errorf("getting parental cluster ref from annotations")
			logger.Error(err, "No parent cluster ref found")
			return ctrl.Result{}, err
		}
		cluster, err = resourcegetter.GetCluster(ctx, r.Client,
			types.NamespacedName{
				Namespace: nodeSet.Namespace,
				Name:      clusterName,
			},
		)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("getting parental cluster: %w", err)
		}
	}
	// endregion Get parental cluster

	if err = r.setUpConditions(ctx, nodeSet); err != nil {
		return ctrl.Result{}, err
	}

	nodeSetValues := values.BuildSlurmNodeSetFrom(
		nodeSet,
		cluster.Name,
		cluster.Spec.Maintenance,
		cluster.Spec.UseDefaultAppArmorProfile,
	)

	if err = r.executeReconciliation(ctx, nodeSet, &nodeSetValues, cluster); err != nil {
		return ctrl.Result{}, err
	}

	// region Maintenance condition
	{
		var condition metav1.Condition
		if check.IsMaintenanceActive(nodeSetValues.Maintenance) {
			condition = metav1.Condition{
				Type:    slurmv1alpha1.ConditionNodeSetStatefulSetTerminated,
				Status:  metav1.ConditionTrue,
				Reason:  "Maintenance",
				Message: "Workers are disabled",
			}
		} else {
			condition = metav1.Condition{
				Type:    slurmv1alpha1.ConditionNodeSetStatefulSetTerminated,
				Status:  metav1.ConditionFalse,
				Reason:  "WorkersEnabled",
				Message: "Workers are enabled",
			}
		}

		if err = r.patchStatus(ctx, nodeSet, func(status *slurmv1alpha1.NodeSetStatus) {
			status.SetCondition(condition)
		}); err != nil {
			return ctrl.Result{}, err
		}
	}
	// endregion Maintenance condition

	// region Validation
	if res, err := r.validateResources(ctx, nodeSet, &nodeSetValues); err != nil {
		logger.Error(err, "Failed to validate Slurm workers")
		return ctrl.Result{}, fmt.Errorf("validating Slurm workers: %w", err)
	} else if res.RequeueAfter > 0 {
		logger.Info("Reconciliation requeued")
		return res, nil
	}
	// endregion Validation

	logger.Info("Finished reconciliation")

	return ctrl.Result{}, nil
}

func (r *NodeSetReconciler) setUpConditions(ctx context.Context, nodeSet *slurmv1alpha1.NodeSet) error {
	patch := client.MergeFrom(nodeSet.DeepCopy())
	needToUpdate := false

	for _, conditionType := range []string{
		slurmv1alpha1.ConditionNodeSetConfigUpdated,
		slurmv1alpha1.ConditionNodeSetConfigDynamicUpdated,
		slurmv1alpha1.ConditionNodeSetStatefulSetUpdated,
		slurmv1alpha1.ConditionNodeSetPodsReady,
		slurmv1alpha1.ConditionNodeSetStatefulSetTerminated,
	} {
		if meta.FindStatusCondition(nodeSet.Status.Conditions, conditionType) != nil {
			continue
		}

		// Don't do
		//	needToUpdate = needToUpdate || nodeSet.Status.SetCondition
		// This will skip the SetCondition call if needToUpdate is already true.
		// Status.SetCondition checks for existing condition and updates only if there is a change.
		updated := nodeSet.Status.SetCondition(metav1.Condition{
			Type:    conditionType,
			Status:  metav1.ConditionUnknown,
			Reason:  "SetUpCondition",
			Message: "The object is not ready yet.",
		})
		needToUpdate = needToUpdate || updated
	}
	if !needToUpdate {
		return nil
	}

	if err := r.Status().Patch(ctx, nodeSet, patch); err != nil {
		log.FromContext(ctx).Error(err, "Failed to patch status")
		return fmt.Errorf("patching %s status: %w", slurmv1alpha1.KindNodeSet, err)
	}

	return nil
}

// executeReconciliation reconciles all resources necessary for deploying Slurm NodeSet workers
func (r NodeSetReconciler) executeReconciliation(
	ctx context.Context,
	nodeSet *slurmv1alpha1.NodeSet,
	nodeSetValues *values.SlurmNodeSet,
	cluster *slurmv1.SlurmCluster,
) error {
	steps := []utils.MultiStepExecutionStep{
		{
			Name: "Security limits ConfigMap",
			Func: func(stepCtx context.Context) error {
				stepLogger := log.FromContext(stepCtx)
				stepLogger.V(1).Info("Reconciling")

				desired := common.RenderConfigMapSecurityLimitsForNodeSet(nodeSetValues)
				stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
				stepLogger.V(1).Info("Rendered")

				if err := r.ConfigMap.Reconcile(stepCtx, nodeSet, &desired); err != nil {
					stepLogger.Error(err, "Failed to reconcile")
					return fmt.Errorf("reconciling worker security limits configmap: %w", err)
				}
				stepLogger.V(1).Info("Reconciled")

				return nil
			},
		},

		{
			Name: "Umbrella worker Service",
			Func: func(stepCtx context.Context) error {
				stepLogger := log.FromContext(stepCtx)
				stepLogger.V(1).Info("Reconciling")

				desired := worker.RenderNodeSetUmbrellaService(nodeSetValues)
				stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
				stepLogger.V(1).Info("Rendered")

				// Cluster has to be the owner of the umbrella service, as it should not be deleted by deleting one of node sets
				if err := r.Service.Reconcile(stepCtx, cluster, &desired, nil); err != nil {
					stepLogger.Error(err, "Failed to reconcile")
					return fmt.Errorf("reconciling umbrella worker Service: %w", err)
				}
				stepLogger.V(1).Info("Reconciled")

				return nil
			},
		},

		{
			Name: "Nodeset worker Service",
			Func: func(stepCtx context.Context) error {
				stepLogger := log.FromContext(stepCtx)
				stepLogger.V(1).Info("Reconciling")

				desired := worker.RenderNodeSetService(nodeSetValues)
				stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
				stepLogger.V(1).Info("Rendered")

				if err := r.Service.Reconcile(stepCtx, nodeSet, &desired, nil); err != nil {
					stepLogger.Error(err, "Failed to reconcile")
					return fmt.Errorf("reconciling worker Service: %w", err)
				}
				stepLogger.V(1).Info("Reconciled")

				return nil
			},
		},

		{
			Name: "Worker StatefulSet",
			Func: func(stepCtx context.Context) error {
				stepLogger := log.FromContext(stepCtx)
				stepLogger.V(1).Info("Reconciling")

				secrets := values.BuildSecretsFrom(&cluster.Spec.Secrets)
				if cluster.Spec.Secrets.SshdKeysName == "" {
					stepLogger.V(1).Info("SshdKeysName is empty. Using default name")
					secrets.SshdKeysName = naming.BuildSecretSSHDKeysName(cluster.Name)
				}

				desired, err := worker.RenderNodeSetStatefulSet(
					nodeSetValues,
					&secrets,
				)
				if err != nil {
					stepLogger.Error(err, "Failed to render")
					return fmt.Errorf("rendering worker StatefulSet: %w", err)
				}
				stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
				stepLogger.V(1).Info("Rendered")

				deps, err := r.getWorkersStatefulSetDependencies(stepCtx, nodeSetValues, cluster)
				if err != nil {
					stepLogger.Error(err, "Failed to retrieve dependencies")
					return fmt.Errorf("retrieving dependencies for worker StatefulSet: %w", err)
				}
				stepLogger.V(1).Info("Retrieved dependencies")

				if err = r.AdvancedStatefulSet.Reconcile(stepCtx, nodeSet, &desired, deps...); err != nil {
					stepLogger.Error(err, "Failed to reconcile")
					return fmt.Errorf("reconciling worker StatefulSet: %w", err)
				}
				stepLogger.V(1).Info("Reconciled")

				return nil
			},
		},
	}

	logger := log.FromContext(ctx)
	if err := utils.ExecuteMultiStep(ctx,
		"Reconciliation of Slurm NodeSet resources",
		utils.MultiStepExecutionStrategyCollectErrors,
		steps...,
	); err != nil {
		logger.Error(err, "Failed to reconcile resources")
		return fmt.Errorf("reconciling Slurm NodeSet resources: %w", err)
	}

	logger.Info("Reconciled resources")
	return nil
}

func (r NodeSetReconciler) validateResources(
	ctx context.Context,
	nodeSet *slurmv1alpha1.NodeSet,
	nodeSetValues *values.SlurmNodeSet,
) (ctrl.Result, error) {
	const requeueDuration = 10 * time.Second
	var (
		res = ctrl.Result{}
	)

	logger := log.FromContext(ctx)

	existing := &kruisev1b1.StatefulSet{}
	err := r.Get(
		ctx,
		types.NamespacedName{
			Namespace: nodeSetValues.ParentalCluster.Namespace,
			Name:      nodeSetValues.StatefulSet.Name,
		},
		existing,
	)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{RequeueAfter: requeueDuration}, nil
		}
		logger.Error(err, "Failed to get StatefulSet")
		return res, fmt.Errorf("getting StatefulSet: %w", err)
	}

	if err = r.patchStatus(ctx, nodeSet, func(status *slurmv1alpha1.NodeSetStatus) {
		status.Replicas = existing.Status.ReadyReplicas
	}); err != nil {
		logger.Error(err, "Failed to update Replicas status")
		return res, fmt.Errorf("updating .Status.Replicas: %w", err)
	}

	{
		var (
			condition metav1.Condition
		)
		if existing.Status.ReadyReplicas == nodeSetValues.StatefulSet.Replicas {
			condition = metav1.Condition{
				Type:    slurmv1alpha1.ConditionNodeSetPodsReady,
				Status:  metav1.ConditionTrue,
				Reason:  "NodeSetReady",
				Message: "NodeSet is ready",
			}
		} else {
			condition = metav1.Condition{
				Type:    slurmv1alpha1.ConditionNodeSetPodsReady,
				Status:  metav1.ConditionFalse,
				Message: "NodeSet is not ready",
				Reason:  "NodeSetNotReady",
			}
			res.RequeueAfter += requeueDuration
		}

		if err = r.patchStatus(ctx, nodeSet, func(status *slurmv1alpha1.NodeSetStatus) {
			status.SetCondition(condition)
		}); err != nil {
			return res, err
		}
	}

	return res, nil
}

func (r NodeSetReconciler) getWorkersStatefulSetDependencies(
	ctx context.Context,
	nodeSet *values.SlurmNodeSet,
	cluster *slurmv1.SlurmCluster,
) ([]metav1.Object, error) {
	var res []metav1.Object

	mungeKeySecret := &corev1.Secret{}
	if err := r.Get(
		ctx,
		types.NamespacedName{
			Namespace: nodeSet.ParentalCluster.Namespace,
			Name:      naming.BuildSecretMungeKeyName(nodeSet.ParentalCluster.Name),
		},
		mungeKeySecret,
	); err != nil {
		return []metav1.Object{}, err
	}
	res = append(res, mungeKeySecret)

	if cluster.Spec.SlurmNodes.Accounting.Enabled {
		slurmdbdSecret := &corev1.Secret{}
		if err := r.Get(
			ctx,
			types.NamespacedName{
				Namespace: nodeSet.ParentalCluster.Namespace,
				Name:      naming.BuildSecretSlurmdbdConfigsName(nodeSet.ParentalCluster.Name),
			},
			slurmdbdSecret,
		); err != nil {
			return []metav1.Object{}, err
		}
		res = append(res, slurmdbdSecret)
	}

	if nodeSet.SupervisorDConfigMapName != "" {
		superviserdConfigMap := &corev1.ConfigMap{}
		err := r.Get(
			ctx,
			types.NamespacedName{
				Namespace: nodeSet.ParentalCluster.Namespace,
				Name:      nodeSet.SupervisorDConfigMapName,
			},
			superviserdConfigMap,
		)
		if err != nil {
			return []metav1.Object{}, err
		}
		res = append(res, superviserdConfigMap)
	}

	if nodeSet.SSHDConfigMapName != "" {
		sshdConfigMap := &corev1.ConfigMap{}
		err := r.Get(
			ctx,
			types.NamespacedName{
				Namespace: nodeSet.ParentalCluster.Namespace,
				Name:      nodeSet.SSHDConfigMapName,
			},
			sshdConfigMap,
		)
		if err != nil {
			return []metav1.Object{}, err
		}
		res = append(res, sshdConfigMap)
	}

	return res, nil
}
