package soperatorchecks

import (
	"context"
	"fmt"
	"maps"
	"time"

	corev1 "k8s.io/api/core/v1"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/controller/reconciler"
	"nebius.ai/slurm-operator/internal/controllerconfig"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

var (
	K8SNodesControllerName = "soperatorchecks.k8snodes"
)

type K8SNodesController struct {
	*reconciler.Reconciler
}

func NewK8SNodesController(
	client client.Client,
	scheme *runtime.Scheme,
	recorder record.EventRecorder,
) *K8SNodesController {
	r := reconciler.NewReconciler(client, scheme, recorder)

	return &K8SNodesController{
		Reconciler: r,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *K8SNodesController) SetupWithManager(mgr ctrl.Manager,
	maxConcurrency int, cacheSyncTimeout time.Duration) error {

	// TODO: common code for predicates
	return ctrl.NewControllerManagedBy(mgr).Named(K8SNodesControllerName).
		For(&corev1.Node{}, builder.WithPredicates(predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				oldNode := e.ObjectOld.(*corev1.Node)
				newNode := e.ObjectNew.(*corev1.Node)

				// Extract the desired conditions from both old and new nodes
				// and compare them to determine if reconciliation is needed
				// based on the conditions changing
				populateConditions := func(conditions []corev1.NodeCondition) map[corev1.NodeConditionType]corev1.NodeCondition {
					condMap := make(map[corev1.NodeConditionType]corev1.NodeCondition)

					for _, condition := range conditions {
						switch condition.Type {
						case consts.SlurmNodeDrain, consts.SlurmNodeReboot, consts.K8SNodeMaintenanceScheduled,
							consts.SoperatorChecksK8SNodeDegraded, consts.SoperatorChecksK8SNodeMaintenance:
							condition := condition

							// Ignore LastHeartbeatTime
							condition.LastHeartbeatTime = v1.Time{}

							condMap[condition.Type] = condition
						}
					}

					return condMap
				}

				oldConditions := populateConditions(oldNode.Status.Conditions)
				newConditions := populateConditions(newNode.Status.Conditions)

				return !maps.Equal(oldConditions, newConditions)
			},
			CreateFunc: func(e event.CreateEvent) bool {
				return true
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				return false
			},
			GenericFunc: func(e event.GenericEvent) bool {
				return true
			},
		})).
		WithOptions(controllerconfig.ControllerOptions(maxConcurrency, cacheSyncTimeout)).
		Complete(r)
}

func (c *K8SNodesController) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName("K8SNodesController.reconcile")
	logger.Info("Running k8s nodes controller")

	k8sNode, err := getK8SNode(ctx, c.Client, req.Name)
	if err != nil {
		logger.V(1).Error(err, "Get k8s node produces an error")
		return ctrl.Result{}, fmt.Errorf("get k8s node: %w", err)
	}

	if err := c.processDrainCondition(ctx, k8sNode); err != nil {
		logger.V(1).Error(err, "Process drain condition produces an error")
		return ctrl.Result{}, fmt.Errorf("process drain condition: %w", err)
	}

	if err := c.processRebootCondition(ctx, k8sNode); err != nil {
		logger.V(1).Error(err, "Process reboot condition produces an error")
		return ctrl.Result{}, fmt.Errorf("process reboot condition: %w", err)
	}
	return ctrl.Result{}, nil
}

func (c *K8SNodesController) processDrainCondition(ctx context.Context, k8sNode *corev1.Node) error {
	logger := log.FromContext(ctx).WithName("K8SNodesController.processDrainCondition")
	logger.Info("processing drain condition")

	var (
		drainCondition       corev1.NodeCondition
		maintenanceCondition corev1.NodeCondition
	)
	for _, cond := range k8sNode.Status.Conditions {
		if cond.Type == consts.SlurmNodeDrain {
			drainCondition = cond
		}
		if cond.Type == consts.SoperatorChecksK8SNodeMaintenance {
			maintenanceCondition = cond
		}
	}

	logger = logger.WithValues("maintenanceCondition", maintenanceCondition, "drainCondition", drainCondition)
	if drainCondition == (corev1.NodeCondition{}) || drainCondition.Status == corev1.ConditionFalse {
		if maintenanceCondition == (corev1.NodeCondition{}) || maintenanceCondition.Status == corev1.ConditionFalse {
			logger.Info("no action needed: no maintenance condition")
			return nil
		}
		logger.Info("setting SlurmNodeDrain: true")
		return setK8SNodeCondition(ctx, c.Client, k8sNode.Name, newNodeCondition(
			consts.SlurmNodeDrain,
			corev1.ConditionTrue,
			consts.ReasonNodeNeedDrain,
			consts.MessageMaintenanceScheduled,
		))
	}
	if drainCondition.Reason != string(consts.ReasonNodeDrained) {
		logger.Info("no action needed: still draining")
		return nil
	}
	if maintenanceCondition.Status != corev1.ConditionTrue {
		logger.Info("setting SlurmNodeDrain: false")
		return setK8SNodeCondition(ctx, c.Client, k8sNode.Name, newNodeCondition(
			consts.SlurmNodeDrain,
			corev1.ConditionFalse,
			consts.ReasonNodeRebooted,
			consts.MessageNodeIsRebooted,
		))
	}

	logger.V(1).Info("deleting k8s node")
	return c.deleteK8SNode(ctx, k8sNode)
}

func (c *K8SNodesController) processRebootCondition(ctx context.Context, k8sNode *corev1.Node) error {
	logger := log.FromContext(ctx).WithName("K8SNodesController.processRebootCondition")
	logger.Info("processing reboot condition")

	var (
		rebootCondition   corev1.NodeCondition
		degradedCondition corev1.NodeCondition
	)
	for _, cond := range k8sNode.Status.Conditions {
		if cond.Type == consts.SlurmNodeReboot {
			rebootCondition = cond
		}
		if cond.Type == consts.SoperatorChecksK8SNodeDegraded {
			degradedCondition = cond
		}
	}
	if rebootCondition == (corev1.NodeCondition{}) || rebootCondition.Status == corev1.ConditionFalse {
		if degradedCondition == (corev1.NodeCondition{}) || degradedCondition.Status == corev1.ConditionFalse {
			logger.Info("no action needed: no reboot reason")
			return nil
		}

		if rebootCondition.Status == corev1.ConditionTrue && degradedCondition.Status == corev1.ConditionTrue &&
			rebootCondition.LastTransitionTime.Time.After(degradedCondition.LastTransitionTime.Time) {

			logger.Info("no action needed: k8s node already was rebooted")
			return nil
		}

		logger.Info("setting SlurmNodeReboot: true")
		return setK8SNodeCondition(ctx, c.Client, k8sNode.Name, newNodeCondition(
			consts.SlurmNodeReboot,
			corev1.ConditionTrue,
			consts.ReasonNodeNeedReboot,
			consts.MessageSlurmNodeDegraded,
		))
	}
	if rebootCondition.Reason != string(consts.ReasonNodeRebooted) {
		logger.Info("no action needed: still rebooting")
		return nil
	}

	logger.Info("setting SlurmNodeReboot: false, SlurmNodeDrain: false, K8SNodeDegraded: false")
	return setK8SNodeConditions(ctx, c.Client, k8sNode.Name,
		newNodeCondition(
			consts.SlurmNodeReboot,
			corev1.ConditionFalse,
			consts.ReasonNodeRebooted,
			consts.MessageNodeIsRebooted,
		),
		newNodeCondition(
			consts.SlurmNodeDrain,
			corev1.ConditionFalse,
			consts.ReasonNodeRebooted,
			consts.MessageNodeIsRebooted,
		),
		newNodeCondition(
			consts.SoperatorChecksK8SNodeDegraded,
			corev1.ConditionFalse,
			consts.ReasonNodeRebooted,
			consts.MessageNodeIsRebooted,
		),
	)
}

func (c *K8SNodesController) deleteK8SNode(ctx context.Context, k8sNode *corev1.Node) error {
	if err := c.Client.Delete(ctx, k8sNode); client.IgnoreNotFound(err) != nil {
		// If the error is not found that means that during reconciliation
		// that node was deleted. We don't need an error in that case.
		return fmt.Errorf("delete k8s node: %w", err)
	}
	return nil
}
