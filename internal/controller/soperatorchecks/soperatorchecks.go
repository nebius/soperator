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

	// node.Status.Conditions belongs to the status subresource, so it is written through
	// Status(), and by patch rather than update: the node comes from the manager cache, whose
	// transform drops fields no controller reads, and an update would write that back wholesale.
	for i, cond := range node.Status.Conditions {
		if cond.Type == condition.Type {

			if cond.Status == condition.Status && cond.Reason == string(condition.Reason) {
				logger.Info("Node already has condition, updating LastHeartbeatTime")
				patch := client.MergeFrom(node.DeepCopy())
				node.Status.Conditions[i].LastHeartbeatTime = metav1.Now()
				return c.Status().Patch(ctx, node, patch)
			}

			logger.Info("Updating existing condition on node")
			patch := client.MergeFrom(node.DeepCopy())
			node.Status.Conditions[i] = condition

			return c.Status().Patch(ctx, node, patch)
		}
	}

	logger.Info("Adding new condition to node")
	// Patch rather than update: the node was read from the manager cache, which trims fields the
	// controllers never read, and a status update would write that truncated status back wholesale.
	// The optimistic lock keeps a concurrent condition write from being lost to the array replacement.
	patch := client.MergeFromWithOptions(node.DeepCopy(), client.MergeFromWithOptimisticLock{})
	node.Status.Conditions = append(node.Status.Conditions, condition)
	if err := c.Status().Patch(ctx, node, patch); err != nil {
		return fmt.Errorf("patch object status: %w", err)
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
