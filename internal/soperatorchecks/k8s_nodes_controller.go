package soperatorchecks

import (
	"context"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	"nebius.ai/slurm-operator/internal/consts"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

type k8sNodesController struct {
	client.Client
}

func newK8SNodesController(c client.Client) *k8sNodesController {
	return &k8sNodesController{
		Client: c,
	}
}

func (c *k8sNodesController) reconcile(ctx context.Context, req ctrl.Request) error {
	logger := log.FromContext(ctx).WithName("k8sNodesController.reconcile")

	logger.Info("starting k8s nodes reconcilation")
	k8sNode, err := getK8SNode(ctx, c.Client, req.Name)
	if err != nil {
		return fmt.Errorf("get k8s node: %w", err)
	}

	if err := c.processDrainCondition(ctx, k8sNode); err != nil {
		return fmt.Errorf("process drain condition: %w", err)
	}

	if err := c.processRebootCondition(ctx, k8sNode); err != nil {
		return fmt.Errorf("process reboot condition: %w", err)
	}
	return nil
}

func (c *k8sNodesController) processDrainCondition(ctx context.Context, k8sNode *corev1.Node) error {
	logger := log.FromContext(ctx).WithName("processDrainCondition")
	logger.Info("processing drain condition")

	var (
		drainCondition       corev1.NodeCondition
		maintenanceCondition corev1.NodeCondition
	)
	for _, cond := range k8sNode.Status.Conditions {
		if cond.Type == consts.SlurmNodeDrain {
			drainCondition = cond
		}
		if cond.Type == consts.K8SNodeMaintenanceScheduled {
			maintenanceCondition = cond
		}
	}

	if drainCondition == (corev1.NodeCondition{}) {
		if maintenanceCondition == (corev1.NodeCondition{}) || maintenanceCondition.Status == corev1.ConditionFalse {
			// No action needed
			logger.Info("no action needed")
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
	if drainCondition.Status != corev1.ConditionTrue ||
		drainCondition.Reason != string(consts.ReasonNodeDrained) ||
		maintenanceCondition.Status != corev1.ConditionTrue {
		// No action needed
		logger.Info("no action needed")
		return nil
	}

	logger.V(1).Info("deleting k8s node")
	return c.deleteK8SNode(ctx, k8sNode)
}

func (c *k8sNodesController) processRebootCondition(ctx context.Context, k8sNode *corev1.Node) error {
	logger := log.FromContext(ctx).WithName("processRebootCondition")
	logger.Info("processing reboot condition")

	var (
		rebootCondition   corev1.NodeCondition
		degradedCondition corev1.NodeCondition
	)
	for _, cond := range k8sNode.Status.Conditions {
		if cond.Type == consts.SlurmNodeReboot {
			rebootCondition = cond
		}
		if cond.Type == consts.K8SNodeDegraded {
			degradedCondition = cond
		}
	}
	if rebootCondition == (corev1.NodeCondition{}) {
		if degradedCondition == (corev1.NodeCondition{}) || degradedCondition.Status == corev1.ConditionFalse {
			// No action needed
			logger.Info("no action needed")
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
	if rebootCondition.Status != corev1.ConditionTrue ||
		rebootCondition.Reason != string(consts.ReasonNodeRebooted) {
		// No action needed
		logger.Info("no action needed")
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
			consts.K8SNodeDegraded,
			corev1.ConditionFalse,
			consts.ReasonNodeRebooted,
			consts.MessageNodeIsRebooted,
		),
	)
}

func (c *k8sNodesController) deleteK8SNode(ctx context.Context, k8sNode *corev1.Node) error {
	if err := c.Client.Delete(ctx, k8sNode); client.IgnoreNotFound(err) != nil {
		// If the error is not found that means that during reconcilation
		// that node was deleted. We don't need an error in that case.
		return fmt.Errorf("delete k8s node: %w", err)
	}
	return nil
}
