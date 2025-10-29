package nodesetcontroller

import (
	"context"
	"fmt"
	"maps"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
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
		//
		clusterName   string
		hasClusterRef bool
	)
	if clusterName, hasClusterRef = nodeSet.GetAnnotations()[consts.AnnotationParentalClusterRefName]; hasClusterRef {
		cluster, err = resourcegetter.GetCluster(ctx, r.Client,
			types.NamespacedName{
				Namespace: nodeSet.Namespace,
				Name:      clusterName,
			},
		)
	} else {
		cluster, err = resourcegetter.GetClusterInNamespace(ctx, r.Client, nodeSet.Namespace)
	}
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("getting parental cluster: %w", err)
	}

	if !hasClusterRef {
		nodeSetBase := nodeSet.DeepCopy()
		maps.Insert(nodeSet.ObjectMeta.Annotations, func(yield func(string, string) bool) {
			if !yield(consts.AnnotationParentalClusterRefName, cluster.Name) {
				return
			}
		})
		if err = r.Patch(ctx, nodeSet, client.MergeFrom(nodeSetBase)); err != nil {
			logger.Error(err, "Failed to patch parental cluster annotation")
			return ctrl.Result{
				RequeueAfter: time.Minute,
			}, fmt.Errorf("patching parental cluster annotation: %w", err)
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

	if err = r.ReconcileNodeSetWorkers(ctx, nodeSet, &nodeSetValues); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("Finished reconciliation")

	return ctrl.Result{}, nil
}

func (r *NodeSetReconciler) setUpConditions(ctx context.Context, nodeSet *slurmv1alpha1.NodeSet) error {
	patch := client.MergeFrom(nodeSet.DeepCopy())

	for _, conditionType := range []string{
		slurmv1alpha1.ConditionNodeSetConfigUpdated,
		slurmv1alpha1.ConditionNodeSetConfigDynamicUpdated,
		slurmv1alpha1.ConditionNodeSetStatefulSetUpdated,
		slurmv1alpha1.ConditionNodeSetPodsReady,
		slurmv1alpha1.ConditionNodeSetStatefulSetTerminated,
	} {
		if cond := meta.FindStatusCondition(nodeSet.Status.Conditions, conditionType); cond == nil {
			continue
		}
		nodeSet.Status.SetCondition(metav1.Condition{
			Type:    conditionType,
			Status:  metav1.ConditionUnknown,
			Reason:  "SetUpCondition",
			Message: "The object is not ready yet.",
		})
	}

	if err := r.Status().Patch(ctx, nodeSet, patch); err != nil {
		log.FromContext(ctx).Error(err, "Failed to patch status")
		return fmt.Errorf("patching %s status: %w", slurmv1alpha1.KindNodeSet, err)
	}

	return nil
}

// ReconcileNodeSetWorkers reconciles all resources necessary for deploying Slurm NodeSet workers
func (r NodeSetReconciler) ReconcileNodeSetWorkers(
	ctx context.Context,
	nodeSet *slurmv1alpha1.NodeSet,
	nodeSetValues *values.SlurmNodeSet,
) error {
	logger := log.FromContext(ctx)

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
				cluster, err := resourcegetter.GetCluster(ctx, r.Client, nodeSetValues.ParentalCluster)
				if err != nil {
					stepLogger.Error(err, "Failed to get parental cluster")
					return fmt.Errorf("getting %s parental cluster %s/%s: %w", slurmv1alpha1.KindNodeSet, nodeSetValues.ParentalCluster.Namespace, nodeSetValues.ParentalCluster.Name, err)
				}

				if err = r.Service.Reconcile(stepCtx, cluster, &desired, nil); err != nil {
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

				cluster, err := resourcegetter.GetCluster(ctx, r.Client, nodeSetValues.ParentalCluster)
				if err != nil {
					stepLogger.Error(err, "Failed to get parental cluster")
					return fmt.Errorf("getting %s parental cluster %s/%s: %w", slurmv1alpha1.KindNodeSet, nodeSetValues.ParentalCluster.Namespace, nodeSetValues.ParentalCluster.Name, err)
				}

				desired, err := worker.RenderNodeSetStatefulSet(
					nodeSetValues,
					ptr.To(values.BuildSecretsFrom(&cluster.Spec.Secrets)),
				)
				if err != nil {
					stepLogger.Error(err, "Failed to render")
					return fmt.Errorf("rendering worker StatefulSet: %w", err)
				}
				stepLogger = stepLogger.WithValues(logfield.ResourceKV(&desired)...)
				stepLogger.V(1).Info("Rendered")

				deps, err := r.getWorkersStatefulSetDependencies(stepCtx, nodeSetValues)
				if err != nil {
					stepLogger.Error(err, "Failed to retrieve dependencies")
					return fmt.Errorf("retrieving dependencies for worker StatefulSet: %w", err)
				}
				stepLogger.V(1).Info("Retrieved dependencies")

				if err = r.AdvancedStatefulSet.Reconcile(stepCtx, cluster, &desired, deps...); err != nil {
					stepLogger.Error(err, "Failed to reconcile")
					return fmt.Errorf("reconciling worker StatefulSet: %w", err)
				}
				stepLogger.V(1).Info("Reconciled")

				return nil
			},
		},
	}

	if err := utils.ExecuteMultiStep(ctx,
		"Reconciliation of Slurm NodeSet workers",
		utils.MultiStepExecutionStrategyCollectErrors,
		steps...,
	); err != nil {
		logger.Error(err, "Failed to reconcile Slurm NodeSet workers")
		return fmt.Errorf("reconciling Slurm NodeSet workers: %w", err)
	}

	logger.Info("Reconciled Slurm NodeSet workers")
	return nil
}

func (r NodeSetReconciler) getWorkersStatefulSetDependencies(
	ctx context.Context,
	nodeSet *values.SlurmNodeSet,
	// clusterValues *values.SlurmCluster,
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

	//if clusterValues.NodeAccounting.Enabled {
	//	slurmdbdSecret := &corev1.Secret{}
	//	if err := r.Get(
	//		ctx,
	//		types.NamespacedName{
	//			Namespace: clusterValues.Namespace,
	//			Name:      naming.BuildSecretSlurmdbdConfigsName(clusterValues.Name),
	//		},
	//		slurmdbdSecret,
	//	); err != nil {
	//		return []metav1.Object{}, err
	//	}
	//	res = append(res, slurmdbdSecret)
	//}

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
