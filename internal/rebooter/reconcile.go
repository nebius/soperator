package rebooter

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/kubereboot/kured/pkg/reboot"
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

type NodeActions struct {
	Reboot  bool
	Drain   bool
	Undrain bool
}

func (r *RebooterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName(ControllerName)
	logger.Info(fmt.Sprintf("Reconciling %s", req.Name))

	node, err := r.getNode(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	nodeActions := r.GetActions(ctx, node)
	logger.V(1).Info("Node actions", "actions", nodeActions)

	if err := r.handleNodeDrain(ctx, node, nodeActions); err != nil {
		return r.handleDrainError(ctx, err)
	}

	if err := r.handleNodeReboot(ctx, node, nodeActions); err != nil {
		return ctrl.Result{}, err
	}

	if err := r.handleNodeUnDrain(ctx, node, nodeActions); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("Reconciliation completed")
	return ctrl.Result{RequeueAfter: r.reconcileTimeout}, nil
}

// getNode returns the node with the given name.
func (r *RebooterReconciler) getNode(ctx context.Context) (*corev1.Node, error) {
	node := &corev1.Node{}
	if err := r.Get(ctx, client.ObjectKey{Name: r.nodeName}, node); err != nil {
		return nil, fmt.Errorf("failed to get node %s: %w", r.nodeName, err)
	}
	return node, nil
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

	logger.Info("Checking if node needs to be undrained")
	if !actions.Drain && !actions.Reboot {
		actions.Undrain = true
	}
	return actions
}

// handleNodeDrain drains the node with the given name if needed.
func (r *RebooterReconciler) handleNodeDrain(ctx context.Context, node *corev1.Node, nodeActions NodeActions) error {
	if nodeActions.Drain {
		if err := r.DrainNodeIfNeeded(ctx, node); err != nil {
			log.FromContext(ctx).V(1).Info("Failed to drain node", "error", err)
			return err
		}
		if err := r.setNodeCondition(ctx, node, consts.SlurmNodeDrain, corev1.ConditionTrue, consts.ReasonNodeDrained, consts.MessageDrained); err != nil {
			return err
		}
	}

	return nil
}

// handleDrainError handles the error that occurred during the node draining process.
func (r *RebooterReconciler) handleDrainError(ctx context.Context, err error) (ctrl.Result, error) {
	var podErr *PodNotEvictableError
	// If some pods are not evicted, requeue the reconciliation.
	// It's better to requeue the reconciliation than to leave the nod in an inconsistent state.
	if errors.As(err, &podErr) {
		log.FromContext(ctx).V(1).Info("Reenqueueing reconciliation due to failed pod eviction")
		return ctrl.Result{RequeueAfter: r.reconcileTimeout}, nil
	}
	return ctrl.Result{}, fmt.Errorf("failed to drain node: %w", err)
}

// handleNodeUnDrain undrains the node with the given name if needed.
func (r *RebooterReconciler) handleNodeUnDrain(ctx context.Context, node *corev1.Node, nodeActions NodeActions) error {
	if nodeActions.Undrain {
		if err := r.UndrainNodeIfNeeded(ctx, node); err != nil {
			log.FromContext(ctx).V(1).Info("Failed to undrain node", "error", err)
			return err
		}
		if err := r.setNodeCondition(ctx, node, consts.SlurmNodeDrain, corev1.ConditionFalse, consts.ReasonNodeUndrained, consts.MessageUndrained); err != nil {
			return err
		}
	}
	return nil
}

// UndrainNodeIfNeeded undrains the node with the given name if needed.
func (r *RebooterReconciler) UndrainNodeIfNeeded(ctx context.Context, node *corev1.Node) error {
	logger := log.FromContext(ctx).WithName("UndrainNodeIfNeeded").WithValues("nodeName", node.Name).V(1)

	logger.Info("Marking node as schedulable")
	if r.IsNodeUnschedulabled(node) {
		setNodeUnschedulable := false
		if err := r.SetNodeUnschedulable(ctx, node, setNodeUnschedulable); err != nil {
			return fmt.Errorf("failed to mark node %s as schedulable: %w", node.Name, err)
		}
	}

	logger.Info("Removing NoExecute taint from node")
	addTaint := false
	if err := r.TaintNodeWithNoExecute(ctx, node, addTaint); err != nil {
		return fmt.Errorf("failed to remove NoExecute taint from node %s: %w", node.Name, err)
	}

	logger.Info("Node undrained")
	return nil
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

// handleNodeReboot reboots the node with the given name if needed.
func (r *RebooterReconciler) handleNodeReboot(ctx context.Context, node *corev1.Node, nodeActions NodeActions) error {
	if nodeActions.Reboot {
		if err := r.RebootNode(ctx, node); err != nil {
			return err
		}
		if err := r.setNodeCondition(ctx, node, consts.SlurmNodeReboot, corev1.ConditionFalse, consts.ReasonNodeRebooted, consts.MessageRebooted); err != nil {
			return err
		}
	}
	if err := r.setNodeCondition(ctx, node, consts.SlurmNodeReboot, corev1.ConditionFalse, consts.ReasonNodeNoRebootNeeded, consts.MessageRebooted); err != nil {
		return err
	}
	return nil
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

// DrainNodeIfNeeded checks if the node needs to be drained and performs the draining process if required.
//
//  1. Sets the SlurmNodeDrain condition to indicate the node is being drained.
//  2. Marks the node as unschedulable if it is not already.
//  3. Applies the NoExecute taint to forcefully evict all pods that do not have a corresponding toleration.
//     This prevents the node from getting stuck if a misconfigured PDB blocks pod eviction.
//  4. Ensures all pods are evicted before completing the process.
//
// Currently, in Soperator, it is assumed that worker pods consume almost all node resources,
// except for a few infrastructure pods (such as CNI), which usually have a toleration for NoExecute.
// Therefore, forceful eviction is necessary to ensure proper node release.
//
// In future releases, a full node drain mechanism may be introduced if needed.
func (r *RebooterReconciler) DrainNodeIfNeeded(ctx context.Context, node *corev1.Node) error {
	logger := log.FromContext(ctx).WithName("DrainNodeIfNeeded").WithValues("nodeName", node.Name).V(1)
	if err := r.setNodeCondition(
		ctx, node, consts.SlurmNodeDrain, corev1.ConditionTrue, consts.ReasonNodeDraining, consts.MessageDraining,
	); err != nil {
		return err
	}
	if !r.IsNodeUnschedulabled(node) {
		logger.Info("Node is not unschedulable")
		setNodeUnschedulable := true
		if err := r.SetNodeUnschedulable(ctx, node, setNodeUnschedulable); err != nil {
			return fmt.Errorf("failed to mark node %s as unschedulable: %w", node.Name, err)
		}
	}

	// TODO: Implement a full node drain mechanism if needed.
	logger.Info("Setting NoExecute taint on node")
	addTaint := true
	if err := r.TaintNodeWithNoExecute(ctx, node, addTaint); err != nil {
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

// SetNodeUnschedulable sets the node with the given name as schedulable or unschedulable.
func (r *RebooterReconciler) SetNodeUnschedulable(ctx context.Context, node *corev1.Node, unschedulable bool) error {
	logger := log.FromContext(ctx).WithName("SetNodeUnschedulable").WithValues("nodeName", node.Name).V(1)
	logger.Info("Setting node schedulable status", "unschedulable", unschedulable)

	node.Spec.Unschedulable = unschedulable
	if err := r.Update(ctx, node); err != nil {
		return fmt.Errorf("failed to update node %s to  is %v: %w", node.Name, unschedulable, err)
	}
	return nil
}

// TaintNodeWithNoExecute taints or untaints the node with the given name with the NoExecute effect.
// NoSchedule means that no new Pods will be scheduled on the tainted node
// unless they have a matching toleration. Pods currently running on the node are not evicted.
func (r *RebooterReconciler) TaintNodeWithNoExecute(ctx context.Context, node *corev1.Node, addTaint bool) error {
	logger := log.FromContext(ctx).WithName("taintNodeWithNoExecute").V(1)
	logger.Info("Modifying NoExecute taint on node", "node name", node.Name, "addTaint", addTaint)

	taint := corev1.Taint{
		Key:    "node.kubernetes.io/NoExecute",
		Value:  "true",
		Effect: corev1.TaintEffectNoExecute,
	}

	if addTaint {
		if r.IsNodeTaintedWithNoExecute(ctx, node) {
			logger.Info("Node already has NoExecute taint", "node name", node.Name)
			return nil
		}
		node.Spec.Taints = append(node.Spec.Taints, taint)
		logger.Info("Adding NoExecute taint to node", "node name", node.Name)
	} else {
		newTaints := []corev1.Taint{}
		for _, t := range node.Spec.Taints {
			if t.Key != taint.Key || t.Effect != taint.Effect {
				newTaints = append(newTaints, t)
			}
		}
		if len(newTaints) == len(node.Spec.Taints) {
			logger.Info("Node does not have NoExecute taint", "node name", node.Name)
			return nil
		}
		node.Spec.Taints = newTaints
		logger.Info("Removing NoExecute taint from node", "node name", node.Name)
	}

	if err := r.Update(ctx, node); err != nil {
		logger.Error(err, "Failed to update node with NoExecute taint modification", "node name", node.Name)
		return err
	}

	logger.Info("Successfully modified NoExecute taint on node", "node name", node.Name)
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
			continue
		}

		if HasTolerationForNoExecute(pod) {
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

// SetNodeCondition sets a custom condition on the node with the given name.
func (r *RebooterReconciler) setNodeCondition(
	ctx context.Context,
	node *corev1.Node,
	conditionType corev1.NodeConditionType,
	status corev1.ConditionStatus,
	reason consts.ReasonConditionType,
	message consts.MessageConditionType,
) error {
	if err := r.SetNodeConditionIfNotExists(ctx, node, conditionType, status, reason, message); err != nil {
		return fmt.Errorf("failed to set node condition for node %s: %w", r.nodeName, err)
	}
	return nil
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
			patch := client.MergeFrom(node.DeepCopy())
			node.Status.Conditions[i] = newNodeCondition

			return r.Status().Patch(ctx, node, patch)
		}
	}

	logger.Info("Adding new condition to node")
	node.Status.Conditions = append(node.Status.Conditions, newNodeCondition)
	return r.UpdateStatus(ctx, node)
}

// RebootNode reboots the node with the given name.
func (r *RebooterReconciler) RebootNode(ctx context.Context, node *corev1.Node) error {
	logger := log.FromContext(ctx).WithName("RebootNodeIfNeeded").WithValues("nodeName", node.Name)
	logger.Info("Starting node reboot")

	rebootCommand := "reboot now"
	rebootCmd, err := reboot.NewCommandRebooter(rebootCommand)
	if err != nil {
		return fmt.Errorf("failed to create reboot command: %w", err)
	}

	if err := rebootCmd.Reboot(); err != nil {
		return fmt.Errorf("failed to reboot node %s: %w", node.Name, err)
	}

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

// This workaround is needed because kube-apiserver expects up-to-date objects for the following update operations.
// And this why we modify the object in-place.
func (r *RebooterReconciler) Update(ctx context.Context, node *corev1.Node, opts ...client.UpdateOption) error {
	if err := r.Client.Update(ctx, node, opts...); err != nil {
		return fmt.Errorf("failed to update object: %w", err)
	}

	if err := r.Get(ctx, client.ObjectKey{Name: node.GetName()}, node); err != nil {
		return fmt.Errorf("failed to get updated node: %w", err)
	}
	return nil
}

func (r *RebooterReconciler) UpdateStatus(ctx context.Context, node *corev1.Node, opts ...client.SubResourceUpdateOption) error {
	if err := r.Status().Update(ctx, node, opts...); err != nil {
		return fmt.Errorf("failed to update object status: %w", err)
	}

	if err := r.Get(ctx, client.ObjectKey{Name: node.GetName()}, node); err != nil {
		return fmt.Errorf("failed to get updated node: %w", err)
	}
	return nil
}
