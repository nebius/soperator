package soperatorchecks

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"nebius.ai/slurm-operator/internal/consts"
)

//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch;delete;update
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;update
//+kubebuilder:rbac:groups=core,resources=nodes/status,verbs=get;update;patch;watch;list
//+kubebuilder:rbac:groups=slurm.nebius.ai,resources=slurmclusters,verbs=get;watch;list
//+kubebuilder:rbac:groups=coordination.k8s.io,resources=leases,verbs=get;watch;list;update;create
//+kubebuilder:rbac:groups=batch,resources=cronjobs,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch;create

func setK8SNodeCondition(
	ctx context.Context,
	c client.Client,
	nodeName string,
	condition corev1.NodeCondition,
) error {
	logger := log.FromContext(ctx).WithName("SetNodeCondition").V(1).
		WithValues(
			"nodeName", nodeName,
			"conditionType", condition.Type,
			"conditionStatus", condition.Status,
			"conditionReason", condition.Reason,
		)

	node, err := getK8SNode(ctx, c, nodeName)
	if err != nil {
		if client.IgnoreNotFound(err) == nil {
			logger.V(1).Info("K8S node not found, skipping condition update")
			return nil
		}
		return err
	}

	// The field node.Status.Conditions belongs to the status of the Node resource.
	// In Kubernetes, the status is considered a "system-owned" object and cannot be
	// modified using a regular Update call.
	// Instead, changes to the status must be made using the Status().Update method.
	for i, cond := range node.Status.Conditions {
		if cond.Type == condition.Type {

			if cond.Status == condition.Status && cond.Reason == string(condition.Reason) {
				logger.Info("Node already has condition, updating LastHeartbeatTime")
				node.Status.Conditions[i].LastHeartbeatTime = metav1.Now()
				patch := client.MergeFrom(node.DeepCopy())
				return c.Status().Patch(ctx, node, patch)
			}

			logger.Info("Updating existing condition on node")
			patch := client.MergeFrom(node.DeepCopy())
			node.Status.Conditions[i] = condition

			return c.Status().Patch(ctx, node, patch)
		}
	}

	logger.Info("Adding new condition to node")
	node.Status.Conditions = append(node.Status.Conditions, condition)
	if err := c.Status().Update(ctx, node); err != nil {
		return fmt.Errorf("failed to update object status: %w", err)
	}

	return nil
}

func setK8SNodeConditions(
	ctx context.Context,
	c client.Client,
	nodeName string,
	conditions ...corev1.NodeCondition,
) error {
	for _, cond := range conditions {
		if err := setK8SNodeCondition(ctx, c, nodeName, cond); err != nil {
			return fmt.Errorf("set k8s node condition: %w", err)
		}
	}
	return nil
}

func newNodeCondition(
	conditionType corev1.NodeConditionType,
	status corev1.ConditionStatus,
	reason consts.ReasonConditionType,
	message consts.MessageConditionType,
) corev1.NodeCondition {
	return corev1.NodeCondition{
		Type:    conditionType,
		Status:  status,
		Reason:  string(reason),
		Message: string(message),
		LastTransitionTime: metav1.Time{
			Time: time.Now(),
		},
		LastHeartbeatTime: metav1.Time{
			Time: time.Now(),
		},
	}
}

func getK8SNode(ctx context.Context, c client.Client, nodeName string) (*corev1.Node, error) {
	node := &corev1.Node{}
	if err := c.Get(ctx, client.ObjectKey{Name: nodeName}, node); err != nil {
		return nil, err
	}
	return node, nil
}

func listK8SNodesWithReader(ctx context.Context, reader client.Reader, limit int64, nextToken string) (corev1.NodeList, error) {
	nodes := &corev1.NodeList{}
	if err := reader.List(ctx, nodes, &client.ListOptions{
		Limit:    limit,
		Continue: nextToken,
	}); err != nil {
		return corev1.NodeList{}, fmt.Errorf("list nodes: %w", err)
	}
	return *nodes, nil
}
