package rebooter

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/mackerelio/go-osstat/uptime"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/controller/reconciler"
	"nebius.ai/slurm-operator/internal/controllerconfig"
)

//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;
//+kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=nodes/status,verbs=get;update

var (
	ControllerName = "rebooter"
)

type RebooterReconciler struct {
	*reconciler.Reconciler
	reconcileTimeout time.Duration
	nodeName         string
}

func NewRebooterReconciler(
	client client.Client,
	scheme *runtime.Scheme,
	recorder record.EventRecorder,
	reconcileTimeout time.Duration,
	nodeName string,
) *RebooterReconciler {
	r := reconciler.NewReconciler(client, scheme, recorder)
	return &RebooterReconciler{
		Reconciler:       r,
		reconcileTimeout: reconcileTimeout,
		nodeName:         nodeName,
	}
}

// PodNotEvictableError represents an error indicating that a pod is not evictable.
type PodNotEvictableError struct {
	PodName string
}

func (e *PodNotEvictableError) Error() string {
	return fmt.Sprintf("pod %s is not evictable", e.PodName)
}

func (r *RebooterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName(ControllerName)
	logger.Info(fmt.Sprintf("Reconciling %s", req.Name))
	node := &corev1.Node{}
	if err := r.Get(ctx, client.ObjectKey{Name: r.nodeName}, node); err != nil {
		return ctrl.Result{RequeueAfter: r.reconcileTimeout}, fmt.Errorf("failed to get node %s: %w", r.nodeName, err)
	}

	nodeActions := r.GetActions(ctx, node)
	logger.V(1).Info("Node actions", "actions", nodeActions)
	if nodeActions.Drain {
		if err := r.DrainNodeIfNeeded(ctx, node); err != nil {
			// If some pods are not evicted, requeue the reconciliation.
			// It's better to requeue the reconciliation than to leave the node in an inconsistent state.
			var podErr *PodNotEvictableError
			if !errors.As(err, &podErr) {
				logger.Info("Reenqueueing reconciliation", "requeueAfter", r.reconcileTimeout, podErr.PodName, "is not evicted")
				return ctrl.Result{RequeueAfter: r.reconcileTimeout}, nil
			}
			return ctrl.Result{RequeueAfter: r.reconcileTimeout}, fmt.Errorf("failed to drain node %s: %w", r.nodeName, err)
		}
		if err := r.SetNodeConditionIfNotExists(
			ctx, node, consts.SlurmNodeDrain, corev1.ConditionFalse, consts.ReasonDrained, consts.MessageDrained,
		); err != nil {
			return ctrl.Result{RequeueAfter: r.reconcileTimeout}, fmt.Errorf("failed to set node condition for node %s: %w", r.nodeName, err)
		}
	}
	if nodeActions.Reboot {
		if err := r.RebootNode(ctx, node); err != nil {
			return ctrl.Result{RequeueAfter: r.reconcileTimeout}, fmt.Errorf("failed to reboot node %s: %w", r.nodeName, err)
		}
	}

	logger.Info("Reconciliation completed")
	return ctrl.Result{}, nil
}

type NodeActions struct {
	Reboot bool
	Drain  bool
}

// GetActions returns the actions that need to be taken on the node with the given name.
func (r *RebooterReconciler) GetActions(ctx context.Context, node *corev1.Node) NodeActions {
	logger := log.FromContext(ctx).WithName("GetActions").WithValues("nodeName", node.Name).V(1)
	actions := NodeActions{}
	logger.Info("Checking if node needs to be drained")
	if r.checkIfNodeNeedsDrain(ctx, node) {
		logger.V(1).Info("Node needs drain")
		actions.Drain = true
	}

	logger.Info("Checking if node needs to be rebooted")
	if r.checkIfNodeNeedsReboot(ctx, node) {
		logger.V(1).Info("Node needs reboot")
		// If the node needs to be rebooted, it also needs to be drained.
		actions.Drain = true
		actions.Reboot = true
	}
	return actions
}

// checkIfNodeNeedsDrain checks if the node with the given name needs to be drained.
func (r *RebooterReconciler) checkIfNodeNeedsDrain(ctx context.Context, node *corev1.Node) bool {
	logger := log.FromContext(ctx).WithName("CheckIfNodeNeedsDrain").WithValues("nodeName", node.Name).V(1)

	if r.CheckNodeCondition(ctx, node, consts.SlurmNodeDrain, corev1.ConditionTrue) {
		logger.Info("Need to drain node")
		return true
	}

	return false
}

// checkIfNodeNeedsReboot checks if the node with the given name needs to be rebooted.
func (r *RebooterReconciler) checkIfNodeNeedsReboot(ctx context.Context, node *corev1.Node) bool {
	logger := log.FromContext(ctx).WithName("CheckIfNodeNeedsReboot").WithValues("nodeName", node.Name).V(1)

	nodeCondSlurmReboot := r.GetNodeConditions(ctx, node, consts.SlurmNodeReboot)
	if nodeCondSlurmReboot == nil {
		return false
	}
	logger.WithValues("nodeCondition", nodeCondSlurmReboot).Info("Checking if node needs reboot")
	return nodeCondSlurmReboot.Status == corev1.ConditionTrue &&
		r.IsUptimeGreaterThanLastTransition(ctx, nodeCondSlurmReboot.LastTransitionTime)
}

// IsUptimeGreaterThanLastTransition checks if the uptime of the node is greater than the last transition time.
func (r *RebooterReconciler) IsUptimeGreaterThanLastTransition(ctx context.Context, lastTransitionTime metav1.Time) bool {
	logger := log.FromContext(ctx).WithName("IsUptimeGreaterThanLastTransition")
	logger.V(1).Info("Checking if uptime is greater than last transition time")
	uptimeDuration, err := uptime.Get()
	if err != nil {
		logger.Error(err, "Failed to get node uptime")
		os.Exit(1)
	}
	nodeStartTime := time.Now().Add(-uptimeDuration)
	return nodeStartTime.Before(lastTransitionTime.Time)
}

// checkNodeCondition checks if the node with the given name has a custom condition set.
func (r *RebooterReconciler) CheckNodeCondition(
	ctx context.Context, node *corev1.Node, nodeConditionType corev1.NodeConditionType, conditionStatus corev1.ConditionStatus,
) bool {
	logger := log.FromContext(ctx).WithName("CheckNodeCondition").WithValues("nodeName", node.Name).V(1)
	logger.Info("Checking node condition")
	for _, condition := range node.Status.Conditions {
		if condition.Type == nodeConditionType && condition.Status == conditionStatus {
			return true
		}
	}

	return false
}

// GetNodeConditions returns the conditions of the node with the given name.
func (r *RebooterReconciler) GetNodeConditions(
	ctx context.Context, node *corev1.Node, conditionType corev1.NodeConditionType,
) *corev1.NodeCondition {
	logger := log.FromContext(ctx).WithName("GetNodeConditions").WithValues("nodeName", node.Name).V(1)
	logger.Info("Getting node conditions")
	for i := range node.Status.Conditions {
		logger.Info("Checking node condition", "conditionType", node.Status.Conditions[i].Type)
		if node.Status.Conditions[i].Type == conditionType {
			return &node.Status.Conditions[i]
		}
	}
	return nil
}

// DrainNodeIfNeeded drains the node with the given name if it is not already drained.
func (r *RebooterReconciler) DrainNodeIfNeeded(ctx context.Context, node *corev1.Node) error {
	logger := log.FromContext(ctx).WithName("DrainNodeIfNeeded").WithValues("nodeName", node.Name).V(1)
	if err := r.SetNodeConditionIfNotExists(
		ctx, node, consts.SlurmNodeDrain, corev1.ConditionTrue, consts.ReasonDraining, consts.MessageDraining,
	); err != nil {
		return fmt.Errorf("failed to set node condition for node %s: %w", node.Name, err)
	}
	if !r.IsNodeUnschedulabled(node) {
		logger.Info("Node is not unschedulable")
		if err := r.MarkNodeUnschedulable(ctx, node); err != nil {
			return fmt.Errorf("failed to mark node %s as unschedulable: %w", node.Name, err)
		}
	}

	logger.Info("Setting NoExecute taint on node")
	if err := r.TaintNodeWithNoExecute(ctx, node); err != nil {
		return fmt.Errorf("failed to taint node %s with NoExecute: %w", node.Name, err)
	}

	logger.Info("Evicting pods from node")
	if err := r.AreAllPodsEvicted(ctx, node.Name); err != nil {
		return err
	}

	logger.Info("Node drained and marked as unschedulable")
	return nil
}

// IsNodeDrained checks if the node with the given name is drained.
func (r *RebooterReconciler) IsNodeUnschedulabled(node *corev1.Node) bool {
	return node.Spec.Unschedulable
}

// MarkNodeUnschedulable marks the node with the given name as unschedulable.
func (r *RebooterReconciler) MarkNodeUnschedulable(ctx context.Context, node *corev1.Node) error {
	logger := log.FromContext(ctx).WithName("MarkNodeUnschedulable").WithValues("nodeName", node.Name).V(1)
	logger.Info("Marking node as unschedulable")

	node.Spec.Unschedulable = true
	if err := r.Update(ctx, node); err != nil {
		return fmt.Errorf("failed to update node %s to unschedulable: %w", node.Name, err)
	}
	return nil
}

// TaintNodeWithNoExecute taints the node with the given name with the NoExecute effect.
// NoSchedule means that no new Pods will be scheduled on the tainted node
// unless they have a matching toleration. Pods currently running on the node are not evicted.
func (r *RebooterReconciler) TaintNodeWithNoExecute(ctx context.Context, node *corev1.Node) error {
	logger := log.FromContext(ctx).WithName("taintNodeWithNoExecute").V(1)
	logger.Info("Adding NoExecute taint to node", "node name", node.Name)

	taint := corev1.Taint{
		Key:    "node.kubernetes.io/NoExecute",
		Value:  "true",
		Effect: corev1.TaintEffectNoExecute,
	}

	if r.IsNodeTaintedWithNoExecute(ctx, node) {
		logger.Info("Node already has NoExecute taint", "node name", node.Name)
		return nil
	}

	node.Spec.Taints = append(node.Spec.Taints, taint)

	if err := r.Client.Update(ctx, node); err != nil {
		logger.Error(err, "Failed to update node with NoExecute taint", "node name", node.Name)
		return err
	}

	logger.Info("Successfully added NoExecute taint to node", "node name", node.Name)
	return nil
}

// IsNodeTaintedWithNoExecute check if the taint already exists
func (r *RebooterReconciler) IsNodeTaintedWithNoExecute(ctx context.Context, node *corev1.Node) bool {
	for _, taint := range node.Spec.Taints {
		if taint.Key == "node.kubernetes.io/NoExecute" {
			return true
		}
	}
	return false
}

// AreAllPodsEvicted checks if all pods on the node with the given name are evicted.
func (r *RebooterReconciler) AreAllPodsEvicted(ctx context.Context, nodeName string) error {
	logger := log.FromContext(ctx).WithName("EvictPodsFromNode").WithValues("nodeName", nodeName).V(1)
	logger.Info("Listing pods on node")

	podList := &corev1.PodList{}
	if err := r.List(ctx, podList, client.MatchingFields{"spec.nodeName": nodeName}); err != nil {
		return fmt.Errorf("failed to list pods on node %s: %w", nodeName, err)
	}

	for _, pod := range podList.Items {
		if IsControlledByDaemonSet(pod) {
			logger.Info("Skipping eviction for pod managed by DaemonSet", "podName", pod.Name)
			continue
		}

		if HasTolerationForNoExecute(pod) {
			logger.Info("Skipping eviction for pod with NoExecute toleration", "podName", pod.Name)
			continue
		}
		return &PodNotEvictableError{PodName: pod.Name}
	}

	return nil
}

// IsControlledByDaemonSet checks if the pod is controlled by a DaemonSet.
// DaemonSet pods should not be evicted. This is because DaemonSet pods are
// expected to run on all nodes and evicting them would cause them to be
// rescheduled on the same node.
func IsControlledByDaemonSet(pod corev1.Pod) bool {
	for _, ownerRef := range pod.OwnerReferences {
		if ownerRef.Kind == "DaemonSet" {
			return true
		}
	}
	return false
}

// HasTolerationForNoExecute checks if the pod has a toleration for NoExecute taint.
// Pods with a toleration for NoExecute taint never get evicted.
func HasTolerationForNoExecute(pod corev1.Pod) bool {
	for _, toleration := range pod.Spec.Tolerations {
		if toleration.Effect == corev1.TaintEffectNoExecute {
			return true
		}
	}
	return false
}

// SetNodeConditionIfNotExists sets a custom condition on the node with the given name if it does not already exist.
func (r *RebooterReconciler) SetNodeConditionIfNotExists(
	ctx context.Context,
	node *corev1.Node,
	conditionType corev1.NodeConditionType,
	status corev1.ConditionStatus,
	reason consts.ReasonConditionType,
	message consts.MessageConditionType,
) error {
	logger := log.FromContext(ctx).WithName("SetNodeConditionIfNotExists").WithValues("nodeName", node.Name).V(1)

	newNodeCondition := corev1.NodeCondition{
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

	// The field node.Status.Conditions belongs to the status of the Node resource.
	// In Kubernetes, the status is considered a "system-owned" object and cannot be
	// modified using a regular Update call.
	// Instead, changes to the status must be made using the Status().Update method.
	for i, cond := range node.Status.Conditions {
		if cond.Type == conditionType {
			if cond.Status == status {
				logger.Info(fmt.Sprintf("Node already has condition %s set to %s", conditionType, status))
				return nil
			}
			logger.Info("Updating existing condition on node")
			node.Status.Conditions[i] = newNodeCondition

			return r.Status().Update(ctx, node)
		}
	}

	logger.Info("Adding new condition to node")
	node.Status.Conditions = append(node.Status.Conditions, newNodeCondition)
	return r.Status().Update(ctx, node)
}

// RebootNode reboots the node with the given name.
func (r *RebooterReconciler) RebootNode(ctx context.Context, node *corev1.Node) error {
	logger := log.FromContext(ctx).WithName("RebootNodeIfNeeded").WithValues("nodeName", node.Name)
	logger.Info("Starting node reboot")
	// TODO: Implement node reboot logic here
	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RebooterReconciler) SetupWithManager(
	mgr ctrl.Manager, maxConcurrency int, cacheSyncTimeout time.Duration, nodeName string,
) error {
	ctx := context.Background()

	// Index pods by node name. This is used to list and evict pods from a specific node.
	if err := mgr.GetFieldIndexer().IndexField(ctx, &corev1.Pod{}, "spec.nodeName", func(rawObj client.Object) []string {
		pod := rawObj.(*corev1.Pod)
		// Check if the pod.Spec.NodeName is the same as the nodeName.
		// If it is, return the nodeName to index the pod by it.
		if pod.Spec.NodeName == nodeName {
			return []string{pod.Spec.NodeName}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to setup field indexer: %w", err)
	}

	// Index the nodes by name
	if err := mgr.GetFieldIndexer().IndexField(ctx, &corev1.Node{}, "metadata.name", func(rawObj client.Object) []string {
		node := rawObj.(*corev1.Node)
		if node.Name == nodeName {
			return []string{node.Name}
		}
		return nil
	}); err != nil {
		return fmt.Errorf("failed to setup node field indexer: %w", err)
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.Node{}, builder.WithPredicates(predicate.Funcs{
			UpdateFunc: func(e event.UpdateEvent) bool {
				// Rebooter is daemonset and should only reconcile the node it is running on
				// so we compare the node name to the NODE_NAME environment variable
				if e.ObjectNew.GetName() != nodeName {
					return false
				}
				oldNode := e.ObjectOld.(*corev1.Node)
				newNode := e.ObjectNew.(*corev1.Node)

				// Extract the desired conditions from both old and new nodes
				// and compare them to determine if reconciliation is needed
				// based on the conditions changing
				var oldDrainCondition, newDrainCondition, oldRebootCondition, newRebootCondition *corev1.NodeCondition
				for i := range oldNode.Status.Conditions {
					if oldNode.Status.Conditions[i].Type == consts.SlurmNodeDrain {
						oldDrainCondition = &oldNode.Status.Conditions[i]
					} else if oldNode.Status.Conditions[i].Type == consts.SlurmNodeReboot {
						oldRebootCondition = &oldNode.Status.Conditions[i]
					}
				}
				for i := range newNode.Status.Conditions {
					if newNode.Status.Conditions[i].Type == consts.SlurmNodeDrain {
						newDrainCondition = &newNode.Status.Conditions[i]
					} else if newNode.Status.Conditions[i].Type == consts.SlurmNodeReboot {
						newRebootCondition = &newNode.Status.Conditions[i]
					}
				}

				// Trigger reconciliation if the Drain condition has changed
				if oldDrainCondition == nil || newDrainCondition == nil || oldDrainCondition.Status != newDrainCondition.Status {
					return true
				}

				// Trigger reconciliation if the Reboot condition has changed
				if oldRebootCondition == nil || newRebootCondition == nil || oldRebootCondition.Status != newRebootCondition.Status {
					return true
				}

				return false
			},
			CreateFunc: func(e event.CreateEvent) bool {
				return e.Object.GetName() == nodeName
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				return false
			},
			GenericFunc: func(e event.GenericEvent) bool {
				return e.Object.GetName() == nodeName
			},
		})).
		WithOptions(controllerconfig.ControllerOptions(maxConcurrency, cacheSyncTimeout)).
		Complete(r)
}
