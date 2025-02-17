package checkcontroller

import (
	"context"
	"errors"
	"fmt"
	"strings"

	slurmapispec "github.com/SlinkyProject/slurm-client/api/v0041"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/ptr"
	"nebius.ai/slurm-operator/internal/consts"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"nebius.ai/slurm-operator/internal/slurmapi"
)

type slurmWorkersController struct {
	client.Client
	slurmAPIClients map[types.NamespacedName]slurmapi.Client
}

// TODO: change clients init to jwtController.
func newSlurmWorkersController(c client.Client, slurmAPIClients map[types.NamespacedName]slurmapi.Client) *slurmWorkersController {
	return &slurmWorkersController{
		Client:          c,
		slurmAPIClients: slurmAPIClients,
	}
}

func (c *slurmWorkersController) reconcile(ctx context.Context, req ctrl.Request) error {
	logger := log.FromContext(ctx).WithName("slurmWorkersController.reconcile").V(1).
		WithValues("k8sNode", req.Name)

	logger.Info("reconciling k8s node")
	k8sNode, err := getK8SNode(ctx, c.Client, req.Name)
	if err != nil {
		return err
	}

	if err := c.processK8SNodeMaintenance(ctx, k8sNode); err != nil {
		return err
	}

	degradedNodes, err := c.findDegradedNodes(ctx, k8sNode.Name)
	if err != nil {
		return err
	}

	logger.Info(fmt.Sprintf("found %d degraded nodes", len(degradedNodes)))
	var errs []error
	for slurmClusterName, nodes := range degradedNodes {
		for _, node := range nodes {
			if err := c.processDegradedNode(ctx, k8sNode, slurmClusterName, node); err != nil {
				errs = append(errs, err)
			}
		}
	}

	return errors.Join(errs...)
}

// TODO: filter also slurmNodes by supported slurm clusters
// in case when 2 slurm nodes are on the same k8s node.
func (c *slurmWorkersController) findDegradedNodes(ctx context.Context, k8sNodeName string) (map[types.NamespacedName][]slurmapi.Node, error) {
	degradedNodes := make(map[types.NamespacedName][]slurmapi.Node)

	for slurmClusterName, slurmAPIClient := range c.slurmAPIClients {
		slurmNodes, err := slurmAPIClient.ListNodes(ctx)
		if err != nil {
			return nil, err
		}

		for _, node := range slurmNodes {
			if node.InstanceID != k8sNodeName {
				// Skip, will process during another node reconcilation
				continue
			}
			if _, ok := node.States[slurmapispec.V0041NodeStateDRAIN]; !ok {
				// Node is not drained, skipping
				continue
			}

			if node.Reason == nil {
				// Node is drained with no reason, skipping
				continue
			}

			for wellKnownReason := range consts.SlurmNodeReasonsMap {
				if strings.Contains(node.Reason.Reason, wellKnownReason) {
					// For simplicity, we keep only well known part
					node.Reason.Reason = wellKnownReason

					nodes := degradedNodes[slurmClusterName]
					nodes = append(nodes, node)
					degradedNodes[slurmClusterName] = nodes
					break
				}
			}
		}
	}

	return degradedNodes, nil
}

func (c *slurmWorkersController) processDegradedNode(
	ctx context.Context,
	k8sNode *corev1.Node,
	slurmClusterName types.NamespacedName,
	node slurmapi.Node,
) error {

	switch node.Reason.Reason {
	case consts.SlurmNodeReasonKillTaskFailed, consts.SlurmNodeReasonDegraded:
		return c.processKillTaskFailed(ctx, k8sNode, slurmClusterName, node)
	case consts.SlurmNodeReasonMaintenanceScheduled:
		return c.processSlurmNodeMaintenance(ctx, k8sNode, slurmClusterName, node.Name)
	default:
		return fmt.Errorf("unknown node reason: node name %s, reason %s, instance id %s",
			node.Name, node.Reason, node.InstanceID)
	}
}

func (c *slurmWorkersController) processKillTaskFailed(
	ctx context.Context,
	k8sNode *corev1.Node,
	slurmClusterName types.NamespacedName,
	slurmNode slurmapi.Node,
) error {

	drainWithCondition := func() error {
		if err := c.drainSlurmNodesWithConditionUpdate(
			ctx,
			slurmNode.InstanceID,
			consts.SlurmNodeReasonDegraded,
			newNodeCondition(
				consts.SlurmNodeReboot,
				corev1.ConditionTrue,
				consts.ReasonNodeNeedReboot,
				"",
			),
		); err != nil {
			return fmt.Errorf("drain slurm nodes: %w", err)
		}

		return nil
	}

	var degradedCondition corev1.NodeCondition
	for _, cond := range k8sNode.Status.Conditions {
		if cond.Type == consts.K8SNodeDegraded {
			degradedCondition = cond
			break
		}
	}

	if degradedCondition == (corev1.NodeCondition{}) {
		// No degraded condition found
		return drainWithCondition()
	}

	if degradedCondition.Status == corev1.ConditionTrue {
		// Node is still rebooting, skip
		return nil
	}
	if slurmNode.Reason.ChangedAt.Before(degradedCondition.LastTransitionTime.Time) {
		return c.undrainSlurmNode(ctx, slurmClusterName, slurmNode.Name)
	}

	return drainWithCondition()
}

func (c *slurmWorkersController) processK8SNodeMaintenance(ctx context.Context, k8sNode *corev1.Node) error {
	drainFn := func() error {
		return c.drainSlurmNodesWithConditionUpdate(
			ctx,
			k8sNode.Name,
			consts.SlurmNodeReasonMaintenanceScheduled,
			newNodeCondition(
				consts.SlurmNodeDrain,
				corev1.ConditionTrue,
				consts.ReasonNodeDraining,
				"",
			),
		)
	}

	return c.processMaintenance(ctx, k8sNode, drainFn, nil)
}

func (c *slurmWorkersController) processSlurmNodeMaintenance(
	ctx context.Context,
	k8sNode *corev1.Node,
	slurmClusterName types.NamespacedName,
	slurmNodeName string) error {

	undrainFn := func() error {
		return c.undrainSlurmNode(ctx, slurmClusterName, slurmNodeName)
	}

	return c.processMaintenance(ctx, k8sNode, nil, undrainFn)
}

func (c *slurmWorkersController) processMaintenance(
	_ context.Context,
	k8sNode *corev1.Node,
	drainFn, undrainFn func() error,
) error {
	if drainFn == nil {
		drainFn = func() error { return nil }
	}
	if undrainFn == nil {
		undrainFn = func() error { return nil }
	}

	var maintenanceCondition corev1.NodeCondition
	for _, cond := range k8sNode.Status.Conditions {
		if cond.Type == consts.K8SNodeMaintenanceScheduled {
			maintenanceCondition = cond
			break
		}
	}

	if maintenanceCondition == (corev1.NodeCondition{}) || maintenanceCondition.Status == corev1.ConditionFalse {
		return undrainFn()
	}

	return drainFn()
}

func (c *slurmWorkersController) drainSlurmNodesWithConditionUpdate(
	ctx context.Context,
	k8sNodeName string,
	reason string,
	condition corev1.NodeCondition,
) error {

	if err := c.drainSlurmNodes(ctx, k8sNodeName, reason); err != nil {
		return fmt.Errorf("drain slurm nodes: %w", err)
	}

	slurmNodesAreFullyDrained, err := c.slurmNodesFullyDrained(ctx, k8sNodeName)
	if err != nil {
		return fmt.Errorf("check that nodes are fully drained: %w", err)
	}

	if !slurmNodesAreFullyDrained {
		// Wait until all slurm nodes are drained.
		return nil
	}

	if err := setK8SNodeCondition(
		ctx,
		c.Client,
		k8sNodeName,
		condition,
	); err != nil {
		return fmt.Errorf("set k8s node condition: %w", err)
	}

	return nil
}

func (c *slurmWorkersController) drainSlurmNodes(
	ctx context.Context,
	k8sNodeName string,
	reason string,
) error {

	podList := &corev1.PodList{}
	if err := c.List(ctx, podList, client.MatchingFields{"spec.nodeName": k8sNodeName}); err != nil {
		return fmt.Errorf("list pods on node %s: %w", k8sNodeName, err)
	}

	var errs []error
	for _, pod := range podList.Items {
		if strings.Contains(pod.Name, "worker-") {
			slurmClusterName := types.NamespacedName{
				Namespace: pod.Namespace,
				Name:      pod.Labels[consts.LabelInstanceKey],
			}

			err := c.drainSlurmNode(ctx, slurmClusterName, pod.Name, reason)
			if err != nil {
				errs = append(errs, err)
			}
		}
	}

	return errors.Join(errs...)
}

func (c *slurmWorkersController) drainSlurmNode(
	ctx context.Context,
	slurmClusterName types.NamespacedName,
	slurmNodeName, reason string,
) error {
	logger := log.FromContext(ctx).WithName("drainSlurmNode").V(1).
		WithValues(
			"slurmNodeName", slurmNodeName,
			"drainReason", reason,
			"slurmCluster", slurmClusterName,
		)
	logger.Info("draining slurm node")

	slurmAPIClient, ok := c.slurmAPIClients[slurmClusterName]
	if !ok {
		return fmt.Errorf("no slurm clusters found with name %s", slurmClusterName)
	}

	resp, err := slurmAPIClient.SlurmV0041PostNodeWithResponse(ctx, slurmNodeName,
		slurmapispec.V0041UpdateNodeMsg{
			Reason: ptr.To(string(reason)),
			State:  ptr.To([]slurmapispec.V0041UpdateNodeMsgState{slurmapispec.V0041UpdateNodeMsgStateDRAIN}),
		},
	)
	if err != nil {
		return fmt.Errorf("post drain slurm node: %w", err)
	}
	if resp.JSON200.Errors != nil && len(*resp.JSON200.Errors) != 0 {
		return fmt.Errorf("post drain returned errors: %v", *resp.JSON200.Errors)
	}

	logger.Info("slurm node state is updated to DRAIN")
	return nil
}

func (c *slurmWorkersController) slurmNodesFullyDrained(
	ctx context.Context,
	k8sNodeName string,
) (bool, error) {
	logger := log.FromContext(ctx).WithName("slurmNodesFullyDrained").V(1)

	logger.Info("checking that slurm nodes are fully drained")
	podList := &corev1.PodList{}
	if err := c.List(ctx, podList, client.MatchingFields{"spec.nodeName": k8sNodeName}); err != nil {
		return false, fmt.Errorf("list pods on node %s: %w", k8sNodeName, err)
	}

	for _, pod := range podList.Items {
		if strings.Contains(pod.Name, "worker-") {
			logger = logger.WithValues("slurmNode", pod.Name, "instanceKey", pod.Labels[consts.LabelInstanceKey])
			logger.Info("found slurm node")

			slurmClusterName := types.NamespacedName{
				Namespace: pod.Namespace,
				Name:      pod.Labels[consts.LabelInstanceKey],
			}

			node, err := c.getSlurmNode(ctx, slurmClusterName, pod.Name)
			if err != nil {
				return false, err
			}
			logger.Info("slurm node", "nodeStates", node.States, "kekuspekus", node)
			if !node.IsIdleDrained() {
				logger.Info("slurm node is not fully drained", "nodeStates", node.States)
				return false, nil
			}
			logger.Info("slurm node is fully drained", "nodeStates", node.States, "kekuspekus", node)
		}
	}

	logger.Info("all slurm nodes are fully drained")
	return true, nil
}

func (c *slurmWorkersController) undrainSlurmNode(
	ctx context.Context,
	slurmClusterName types.NamespacedName,
	slurmNodeName string,
) error {
	logger := log.FromContext(ctx).WithName("undrainSlurmNode").V(1).
		WithValues(
			"slurmNodeName", slurmNodeName,
			"slurmCluster", slurmClusterName,
		)
	logger.Info("undraining slurm node")

	slurmAPIClient, ok := c.slurmAPIClients[slurmClusterName]
	if !ok {
		return fmt.Errorf("no slurm clusters with name %s", slurmClusterName)
	}

	resp, err := slurmAPIClient.SlurmV0041PostNodeWithResponse(ctx, slurmNodeName,
		slurmapispec.V0041UpdateNodeMsg{
			State: ptr.To([]slurmapispec.V0041UpdateNodeMsgState{slurmapispec.V0041UpdateNodeMsgStateRESUME}),
		},
	)
	if err != nil {
		return fmt.Errorf("post undrain slurm node: %w", err)
	}
	if resp.JSON200.Errors != nil && len(*resp.JSON200.Errors) != 0 {
		return fmt.Errorf("post undrain returned errors: %v", *resp.JSON200.Errors)
	}

	logger.Info("slurm node state is updated to RESUME")
	return nil
}

func (c *slurmWorkersController) getSlurmNode(
	ctx context.Context,
	slurmClusterName types.NamespacedName,
	slurmNodeName string,
) (slurmapi.Node, error) {

	slurmAPIClient, ok := c.slurmAPIClients[slurmClusterName]
	if !ok {
		return slurmapi.Node{}, fmt.Errorf("no slurm clusters with name %s", slurmClusterName)
	}

	node, err := slurmAPIClient.GetNode(ctx, slurmNodeName)
	if err != nil {
		return slurmapi.Node{}, fmt.Errorf("get node: %w", err)
	}

	return node, nil
}
