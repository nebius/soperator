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
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/record"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/source"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/controller/reconciler"
	"nebius.ai/slurm-operator/internal/controllerconfig"
)

//+kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch;update;patch
//+kubebuilder:rbac:groups=core,resources=nodes/status,verbs=get;update;patch;watch;list
//+kubebuilder:rbac:groups=core,resources=nodes/proxy,verbs=get

var (
	ControllerName = "rebooter"
)

type RebooterParams struct {
	ReconcileTimeout time.Duration
	NodeName         string
	EvictionMethod   consts.RebooterMethod
}

type RebooterReconciler struct {
	*reconciler.Reconciler
	APIReader        client.Reader
	reconcileTimeout time.Duration
	nodeName         string
	evictionMethod   consts.RebooterMethod
	NodePodsFetcher  NodePodsFetcher
}

func NewRebooterReconciler(
	c client.Client,
	apiReader client.Reader,
	scheme *runtime.Scheme,
	recorder record.EventRecorder,
	rebooterParams RebooterParams,
	nodePodsFetcher NodePodsFetcher,
) *RebooterReconciler {
	r := reconciler.NewReconciler(c, scheme, recorder)
	return &RebooterReconciler{
		Reconciler:       r,
		APIReader:        apiReader,
		reconcileTimeout: rebooterParams.ReconcileTimeout,
		nodeName:         rebooterParams.NodeName,
		evictionMethod:   rebooterParams.EvictionMethod,
		NodePodsFetcher:  nodePodsFetcher,
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
	Reboot bool
	Drain  bool
}

func (r *RebooterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName(ControllerName)
	logger.Info(fmt.Sprintf("Reconciling %s", req.Name))

	node, err := r.getNode(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	nodeActions, err := r.GetActions(ctx, node)
	if err != nil {
		return ctrl.Result{}, err
	}
	logger.V(1).Info("Node actions", "actions", nodeActions)

	logger.V(1).Info("Starting handling node drain")
	if err := r.handleNodeDrain(ctx, node, nodeActions); err != nil {
		logger.V(1).Info("Failed to drain node", "error", err)
		return r.handleDrainError(ctx, err)
	}

	logger.V(1).Info("Starting handling node reboot")
	if err := r.handleNodeReboot(ctx, node, nodeActions); err != nil {
		if apierrors.IsConflict(err) {
			logger.V(1).Info("Conflict during reboot handling, requeueing")
			return ctrl.Result{Requeue: true}, nil
		}
		return ctrl.Result{}, err
	}

	logger.Info("Reconciliation completed")
	return ctrl.Result{RequeueAfter: r.reconcileTimeout}, nil
}

// getNode returns the node with the given name directly from the API server,
// bypassing the informer cache.
func (r *RebooterReconciler) getNode(ctx context.Context) (*corev1.Node, error) {
	node := &corev1.Node{}
	if err := r.APIReader.Get(ctx, client.ObjectKey{Name: r.nodeName}, node); err != nil {
		return nil, fmt.Errorf("failed to get node %s: %w", r.nodeName, err)
	}
	return node, nil
}

// GetActions returns the actions that need to be taken on the node with the given name.
func (r *RebooterReconciler) GetActions(ctx context.Context, node *corev1.Node) (NodeActions, error) {
	logger := log.FromContext(ctx).WithName("GetActions").WithValues("nodeName", node.Name).V(1)
	actions := NodeActions{}

	nodeDrainCondition := r.GetNodeConditions(ctx, node, consts.SlurmNodeDrain)
	logger.Info("Checking if node needs to be drained")
	if nodeDrainCondition != nil && r.checkIfNodeNeedsDrain(ctx, nodeDrainCondition) {
		logger.V(1).Info("Node needs drain")
		actions.Drain = true
	}

	nodeRebootCondition := r.GetNodeConditions(ctx, node, consts.SlurmNodeReboot)
	logger.Info("Checking if node needs to be rebooted")
	if nodeRebootCondition != nil {
		if !r.IsUptimeGreaterThanLastTransition(ctx, nodeRebootCondition.LastTransitionTime) {
			logger.Info("Node does not need to be rebooted")

			// If node just has been rebooted, it's still has NoExecute taint, so we should undrain it.
			logger.Info("Checking if node needs to be undrained")
			if r.checkIfNodeNeedsReboot(ctx, nodeRebootCondition) {
				if err := r.handleNodeUnDrain(ctx, node); err != nil {
					return NodeActions{}, err
				}
			}

			err := r.setNodeCondition(ctx, node, consts.SlurmNodeReboot, corev1.ConditionTrue, consts.ReasonNodeRebooted, consts.MessageRebooted)
			if err != nil {
				return NodeActions{}, fmt.Errorf("set node condition: %w", err)
			}
		} else if r.checkIfNodeNeedsReboot(ctx, nodeRebootCondition) {
			logger.V(1).Info("Node needs reboot")
			// If the node needs to be rebooted, it also needs to be drained.
			actions.Drain = true
			actions.Reboot = true
		}
	}

	return actions, nil
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
	if apierrors.IsConflict(err) {
		log.FromContext(ctx).V(1).Info("Conflict during drain handling, requeueing")
		return ctrl.Result{Requeue: true}, nil
	}
	return ctrl.Result{}, fmt.Errorf("failed to drain node: %w", err)
}

// handleNodeUnDrain undrains the node with the given name if needed.
func (r *RebooterReconciler) handleNodeUnDrain(ctx context.Context, node *corev1.Node) error {
	if err := r.UndrainNodeIfNeeded(ctx, node); err != nil {
		log.FromContext(ctx).V(1).Info("Failed to undrain node", "error", err)
		return err
	}
	if err := r.setNodeCondition(ctx, node, consts.SlurmNodeDrain, corev1.ConditionFalse, consts.ReasonNodeUndrained, consts.MessageUndrained); err != nil {
		return err
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
func (r *RebooterReconciler) checkIfNodeNeedsDrain(ctx context.Context, nodeCondition *corev1.NodeCondition) bool {
	return r.CheckNodeCondition(ctx, nodeCondition, consts.SlurmNodeDrain, corev1.ConditionTrue)
}

// handleNodeReboot reboots the node with the given name if needed.
func (r *RebooterReconciler) handleNodeReboot(ctx context.Context, node *corev1.Node, nodeActions NodeActions) error {
	if nodeActions.Reboot {
		if err := r.setNodeCondition(ctx, node, consts.SlurmNodeReboot, corev1.ConditionTrue, consts.ReasonNodeRebooting, consts.MessageRebooting); err != nil {
			return err
		}
		if err := r.RebootNode(ctx, node); err != nil {
			return err
		}
	}
	return nil
}

// checkIfNodeNeedsReboot checks if the node with the given name needs to be rebooted.
func (r *RebooterReconciler) checkIfNodeNeedsReboot(ctx context.Context, nodeCondition *corev1.NodeCondition) bool {
	logger := log.FromContext(ctx).WithName("CheckIfNodeNeedsReboot").WithValues("nodeName", r.nodeName).V(1)

	if nodeCondition.Reason == string(consts.ReasonNodeRebooted) {
		logger.Info("Node already rebooted")
		return false
	}
	return r.CheckNodeCondition(ctx, nodeCondition, consts.SlurmNodeReboot, corev1.ConditionTrue)
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
	ctx context.Context, nodeCondition *corev1.NodeCondition, nodeConditionType corev1.NodeConditionType, conditionStatus corev1.ConditionStatus,
) bool {
	logger := log.FromContext(ctx).WithName("CheckNodeCondition").WithValues("nodeName", r.nodeName).V(1)
	logger.Info("Checking node condition")
	return nodeCondition != nil && nodeCondition.Type == nodeConditionType && nodeCondition.Status == conditionStatus
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
	if r.evictionMethod == consts.RebooterEvict {
		logger.Info("Setting NoExecute taint on node")
		addTaint := true
		if err := r.TaintNodeWithNoExecute(ctx, node, addTaint); err != nil {
			return fmt.Errorf("failed to taint node %s with NoExecute: %w", node.Name, err)
		}
	}
	// TODO: Implement a full node drain mechanism if needed.
	// This part should be implemented later when the full node drain mechanism is required.
	if r.evictionMethod == consts.RebooterDrain {
		logger.Info("Full node drain mechanism is not implemented yet")
		return nil
	}

	logger.Info("Evicting pods from node")
	if err := r.AreAllPodsEvicted(ctx, node); err != nil {
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

	if err := r.patchNodeSpecWithRetry(ctx, node, func(currentNode *corev1.Node) (bool, error) {
		if currentNode.Spec.Unschedulable == unschedulable {
			return false, nil
		}
		currentNode.Spec.Unschedulable = unschedulable
		return true, nil
	}); err != nil {
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

	if err := r.patchNodeSpecWithRetry(ctx, node, func(currentNode *corev1.Node) (bool, error) {
		if addTaint {
			if r.IsNodeTaintedWithNoExecute(currentNode) {
				logger.Info("Node already has NoExecute taint", "node name", currentNode.Name)
				return false, nil
			}
			currentNode.Spec.Taints = append(currentNode.Spec.Taints, taint)
			logger.Info("Adding NoExecute taint to node", "node name", currentNode.Name)
			return true, nil
		}

		newTaints := make([]corev1.Taint, 0, len(currentNode.Spec.Taints))
		for _, t := range currentNode.Spec.Taints {
			if t.Key != taint.Key || t.Effect != taint.Effect {
				newTaints = append(newTaints, t)
			}
		}
		if len(newTaints) == len(currentNode.Spec.Taints) {
			logger.Info("Node does not have NoExecute taint", "node name", currentNode.Name)
			return false, nil
		}
		currentNode.Spec.Taints = newTaints
		logger.Info("Removing NoExecute taint from node", "node name", currentNode.Name)
		return true, nil
	}); err != nil {
		logger.Error(err, "Failed to update node with NoExecute taint modification", "node name", node.Name)
		return err
	}

	logger.Info("Successfully modified NoExecute taint on node", "node name", node.Name)
	return nil
}

func (r *RebooterReconciler) patchNodeSpecWithRetry(
	ctx context.Context,
	node *corev1.Node,
	mutate func(*corev1.Node) (bool, error),
) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		currentNode := &corev1.Node{}
		if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(node), currentNode); err != nil {
			return fmt.Errorf("get latest node %s: %w", node.Name, err)
		}

		base := currentNode.DeepCopy()
		changed, err := mutate(currentNode)
		if err != nil {
			return err
		}
		if !changed {
			*node = *currentNode
			return nil
		}

		if err := r.Client.Patch(ctx, currentNode, client.MergeFrom(base)); err != nil {
			return fmt.Errorf("patch object: %w", err)
		}

		*node = *currentNode
		return nil
	})
}

// IsNodeTaintedWithNoExecute check if the taint already exists
func (r *RebooterReconciler) IsNodeTaintedWithNoExecute(node *corev1.Node) bool {
	for _, taint := range node.Spec.Taints {
		if taint.Key == "node.kubernetes.io/NoExecute" {
			return true
		}
	}
	return false
}

// AreAllPodsEvicted checks if all pods on the node are evicted by querying
// the API server's node proxy endpoint. This avoids loading full PodList
// objects into the informer cache while staying on the API server's auth path.
func (r *RebooterReconciler) AreAllPodsEvicted(ctx context.Context, node *corev1.Node) error {
	logger := log.FromContext(ctx).WithName("AreAllPodsEvicted").WithValues("nodeName", node.Name).V(1)

	podList, err := r.NodePodsFetcher.GetPodsOnNode(ctx, node.Name)
	if err != nil {
		return fmt.Errorf("fetch pods on node %s: %w", node.Name, err)
	}

	logger.Info("Checking pods via API server proxy", "count", len(podList.Items))
	for i := range podList.Items {
		pod := &podList.Items[i]
		if IsControlledByDaemonSet(*pod) {
			continue
		}
		if HasTolerationForNoExecute(*pod) {
			continue
		}
		if HasTolerationForExists(*pod) {
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

// HasTolerationForExists checks if the pod has a toleration with operator Exists.
// Pods with such a toleration can tolerate any taint with the same key.
func HasTolerationForExists(pod corev1.Pod) bool {
	for _, toleration := range pod.Spec.Tolerations {
		if toleration.Operator == corev1.TolerationOpExists {
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
		return fmt.Errorf("set node condition for node %s: %w", r.nodeName, err)
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
	return r.patchNodeStatusWithRetry(ctx, node, func(currentNode *corev1.Node) (bool, error) {
		for i, cond := range currentNode.Status.Conditions {
			if cond.Type != conditionType {
				continue
			}

			if cond.Status == status && cond.Reason == string(reason) {
				logger.Info(fmt.Sprintf("Node already has condition %s set to %s", conditionType, status))
				newNodeCondition.LastTransitionTime = cond.LastTransitionTime
			}

			logger.Info("Updating existing condition on node")
			currentNode.Status.Conditions[i] = newNodeCondition
			return true, nil
		}

		logger.Info("Adding new condition to node")
		currentNode.Status.Conditions = append(currentNode.Status.Conditions, newNodeCondition)
		return true, nil
	})
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
// No node informer is registered — this avoids a LIST+WATCH against the API server
// at startup. Instead, one GenericEvent is sent on startCh to kick off the first
// reconcile; subsequent cycles are driven by ctrl.Result{RequeueAfter: ...}.
func (r *RebooterReconciler) SetupWithManager(
	mgr ctrl.Manager, maxConcurrency int, cacheSyncTimeout time.Duration, nodeName string,
) error {
	startCh := make(chan event.GenericEvent, 1)
	startCh <- event.GenericEvent{
		Object: &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}},
	}

	return ctrl.NewControllerManagedBy(mgr).
		Named(ControllerName).
		WatchesRawSource(source.Channel(
			startCh,
			handler.EnqueueRequestsFromMapFunc(func(_ context.Context, _ client.Object) []ctrl.Request {
				return []ctrl.Request{{NamespacedName: types.NamespacedName{Name: nodeName}}}
			}),
		)).
		WithOptions(controllerconfig.ControllerOptions(maxConcurrency, cacheSyncTimeout)).
		Complete(r)
}

// Update writes the node spec to the API server. controller-runtime mutates the
// object in-place with the server response (including the new resourceVersion),
// so no extra Get is needed to keep subsequent updates consistent.
func (r *RebooterReconciler) Update(ctx context.Context, node *corev1.Node, opts ...client.UpdateOption) error {
	if err := r.Client.Update(ctx, node, opts...); err != nil {
		return fmt.Errorf("failed to update object: %w", err)
	}
	return nil
}

func (r *RebooterReconciler) UpdateStatus(ctx context.Context, node *corev1.Node, opts ...client.SubResourceUpdateOption) error {
	if err := r.Status().Update(ctx, node, opts...); err != nil {
		return fmt.Errorf("failed to update object status: %w", err)
	}
	return nil
}

func (r *RebooterReconciler) patchNodeStatusWithRetry(
	ctx context.Context,
	node *corev1.Node,
	mutate func(*corev1.Node) (bool, error),
) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		currentNode := &corev1.Node{}
		if err := r.APIReader.Get(ctx, client.ObjectKeyFromObject(node), currentNode); err != nil {
			return fmt.Errorf("get latest node %s: %w", node.Name, err)
		}

		base := currentNode.DeepCopy()
		changed, err := mutate(currentNode)
		if err != nil {
			return err
		}
		if !changed {
			*node = *currentNode
			return nil
		}

		if err := r.Status().Patch(ctx, currentNode, client.MergeFrom(base)); err != nil {
			return fmt.Errorf("patch object status: %w", err)
		}

		*node = *currentNode
		return nil
	})
}
