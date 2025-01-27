package checkcontroller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/controller/reconciler"
)

//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;delete;update
//+kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch
//+kubebuilder:rbac:groups=core,resources=nodes/status,verbs=get;update

var (
	ControllerName     = "checkcontroller"
	SlurmNodeCondition = corev1.NodeConditionType("SlurmDown")
)

type CheckControllerReconciler struct {
	*reconciler.Reconciler
	reconcileTimeout time.Duration
	ScontrolRunner   ScontrolRunner
}

func NewCheckControllerReconciler(client client.Client, scheme *runtime.Scheme, recorder record.EventRecorder, reconcileTimeout time.Duration) *CheckControllerReconciler {
	r := reconciler.NewReconciler(client, scheme, recorder)
	return &CheckControllerReconciler{
		Reconciler:       r,
		reconcileTimeout: reconcileTimeout,
		ScontrolRunner:   ScontrolRunner{},
	}
}

func (r *CheckControllerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName(ControllerName)
	logger.Info(fmt.Sprintf("Reconciling %s/%s", req.Namespace, req.Name))

	logger.V(1).Info("Running scontrol command")
	output, err := r.ScontrolRunner.ShowNodes(ctx)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("error running scontrol command: %w", err)
	}

	logger.V(1).Info("Unmarshaling JSON")
	slurmData, err := unmarshalSlurmJSON(output)
	if err != nil {
		return ctrl.Result{RequeueAfter: r.reconcileTimeout}, fmt.Errorf("error unmarshaling JSON: %w", err)
	}
	if len(slurmData) == 0 {
		logger.V(1).Info("No data found")
		return ctrl.Result{RequeueAfter: r.reconcileTimeout}, nil
	}

	logger.V(1).Info("Finding nodes by state and reason")
	matchingNodes, err := findNodesByStateAndReason(slurmData)
	if err != nil {
		return ctrl.Result{RequeueAfter: r.reconcileTimeout}, fmt.Errorf("error finding nodes by state and reason: %w", err)
	}

	logger.V(1).Info("Matching nodes", "nodes", matchingNodes)
	if len(matchingNodes) == 0 {
		logger.V(1).Info("No matching nodes")
		return ctrl.Result{RequeueAfter: r.reconcileTimeout}, nil
	}

	unschedulableNodes, errs := r.processMatchingNodes(ctx, matchingNodes, req.Namespace)
	if len(errs) > 0 {
		return ctrl.Result{RequeueAfter: r.reconcileTimeout}, fmt.Errorf("errors occurred: %v", errs)
	}

	logger.V(1).Info("Updating conditions on nodes")
	err = r.updateKubeNodeConditionSlurm(ctx, unschedulableNodes)
	if err != nil {
		return ctrl.Result{RequeueAfter: r.reconcileTimeout}, fmt.Errorf("error updating conditions on nodes: %w", err)
	}

	return ctrl.Result{RequeueAfter: r.reconcileTimeout}, nil
}

func (r *CheckControllerReconciler) processMatchingNodes(ctx context.Context, nodes []string, namespace string) (map[string]*corev1.Node, []error) {
	var errs []error

	unschedulableNodes := map[string]*corev1.Node{}

	for _, workerName := range nodes {
		podNodeName, err := r.GetPodNodeName(ctx, workerName, namespace)
		if err != nil {
			errs = append(errs, fmt.Errorf("error getting pod node name: %w", err))
			continue
		}
		node, err := r.GetNode(ctx, podNodeName)
		if err != nil {
			errs = append(errs, fmt.Errorf("error getting node: %w", err))
			continue
		}
		unschedulableNodes[node.Name] = node

		needsReboot, err := r.checkIfNodeNeedsReboot(ctx, node)
		if err != nil {
			errs = append(errs, fmt.Errorf("error checking if node needs reboot: %w", err))
			continue
		}
		if !needsReboot {
			continue
		}
		if err := r.rebootNode(ctx, node, workerName, namespace); err != nil {
			errs = append(errs, fmt.Errorf("error rebooting node: %w", err))
		}
	}

	return unschedulableNodes, errs
}

// checkIfNodeNeedsReboot checks if the node with the given name needs to be rebooted.
func (r *CheckControllerReconciler) checkIfNodeNeedsReboot(ctx context.Context, node *corev1.Node) (bool, error) {
	logger := log.FromContext(ctx).WithName("CheckIfNodeNeedsReboot").WithValues("nodeName", node.Name)

	conditionExist, err := r.CheckNodeCondition(ctx, node, SlurmNodeCondition, corev1.ConditionTrue)
	if err != nil {
		return false, fmt.Errorf("error checking node condition: %w", err)
	}
	if conditionExist {
		logger.V(1).Info("Node condition exists")
		return false, nil
	}

	logger.V(1).Info("Checking if node is drained")
	if !r.IsNodeDrained(node) {
		logger.V(1).Info("Node is not drained")
		return false, nil
	}

	return false, nil
}

// rebootNode reboots the node with the given name in the given namespace.
func (r *CheckControllerReconciler) rebootNode(ctx context.Context, node *corev1.Node, workerName, namespace string) error {
	logger := log.FromContext(ctx).WithName("RebootNode").WithValues("workerName", workerName, "nodeName", node.Name)

	if err := r.DrainNodeIfNeeded(ctx, node); err != nil {
		return err
	}

	if err := r.CreateRebooterPodIfNeeded(ctx, workerName, node.Name, namespace); err != nil {
		return err
	}

	if err := r.SetNodeConditionIfNotExists(ctx, node, SlurmNodeCondition, corev1.ConditionTrue, "Node is being rebooted"); err != nil {
		return err
	}

	logger.V(1).Info("Node condition set")
	return nil
}

// DrainNodeIfNeeded drains the node with the given name if it is not already drained.
func (r *CheckControllerReconciler) DrainNodeIfNeeded(ctx context.Context, node *corev1.Node) error {
	logger := log.FromContext(ctx).WithName("DrainNodeIfNeeded").WithValues("nodeName", node.Name)

	if r.IsNodeDrained(node) {
		logger.V(1).Info("Node is already drained")
		return nil
	}

	logger.V(1).Info("Draining node")
	if err := r.DrainNode(ctx, node); err != nil {
		return fmt.Errorf("failed to drain node: %w", err)
	}

	logger.V(1).Info("Marking node as unschedulable")
	node.Spec.Unschedulable = true
	if err := r.Update(ctx, node); err != nil {
		return fmt.Errorf("failed to update nodeto unschedulable: %w", err)
	}

	logger.V(1).Info("Node drained and marked as unschedulable")
	return nil
}

// CreateRebooterPodIfNeeded creates a rebooter pod for the node with the given name if it does not already exist.
func (r *CheckControllerReconciler) CreateRebooterPodIfNeeded(ctx context.Context, workerName, nodeName, namespace string) error {
	logger := log.FromContext(ctx).WithName("CreateRebooterPodIfNeeded").WithValues("workerName", workerName, "nodeName", nodeName)

	logger.V(1).Info("Checking for existing rebooter pod")
	existingPod, err := r.GetRebooterPod(ctx, nodeName, namespace)
	if err == nil && existingPod != nil {
		logger.Info("Rebooter pod exists", "podName", existingPod.Name)
		err = r.Delete(ctx, existingPod)
		if err != nil {
			return fmt.Errorf("failed to delete existing rebooter pod: %w", err)
		}
		return nil
	}

	logger.V(1).Info("Creating rebooter pod")
	if err := r.CreateRebooterPod(ctx, workerName, nodeName, namespace); err != nil {
		return fmt.Errorf("failed to create rebooter pod for node %s: %w", nodeName, err)
	}

	logger.V(1).Info("Rebooter pod created successfully")
	return nil
}

// GetRebooterPod gets the rebooter pod for the node with the given name in the given namespace.
func (r *CheckControllerReconciler) GetRebooterPod(ctx context.Context, nodeName, namespace string) (*corev1.Pod, error) {
	pod := &corev1.Pod{}
	err := r.Get(ctx, client.ObjectKey{Name: fmt.Sprintf("node-rebooter-%s", nodeName), Namespace: namespace}, pod)
	if err != nil {
		return nil, fmt.Errorf("error getting rebooter pod: %w", err)
	}
	return pod, nil
}

// SetNodeConditionIfNotExists sets a custom condition on the node with the given name if it does not already exist.
func (r *CheckControllerReconciler) SetNodeConditionIfNotExists(
	ctx context.Context,
	node *corev1.Node,
	conditionType corev1.NodeConditionType,
	status corev1.ConditionStatus,
	message string,
) error {
	logger := log.FromContext(ctx).WithName("SetNodeConditionIfNotExists").WithValues("nodeName", node.Name)

	// The field node.Status.Conditions belongs to the status of the Node resource.
	// In Kubernetes, the status is considered a "system-owned" object and cannot be
	// modified using a regular Update call.
	// Instead, changes to the status must be made using the Status().Update method.
	for i, cond := range node.Status.Conditions {
		if cond.Type == conditionType {
			if cond.Status == status {
				logger.V(1).Info(fmt.Sprintf("Node already has condition %s set to %s", conditionType, status))
				return nil
			}
			logger.V(1).Info("Updating existing condition to %s", status)
			node.Status.Conditions[i] = corev1.NodeCondition{
				Type:    conditionType,
				Status:  status,
				Message: message,
				LastTransitionTime: metav1.Time{
					Time: time.Now(),
				},
				LastHeartbeatTime: metav1.Time{
					Time: time.Now(),
				},
			}
			return r.Status().Update(ctx, node)
		}
	}

	logger.V(1).Info("Adding new condition to node")
	node.Status.Conditions = append(node.Status.Conditions, corev1.NodeCondition{
		Type:    conditionType,
		Status:  status,
		Message: message,
		LastTransitionTime: metav1.Time{
			Time: time.Now(),
		},
		LastHeartbeatTime: metav1.Time{
			Time: time.Now(),
		},
	})
	return r.Status().Update(ctx, node)
}

// GetPodNodeName gets the node name of the pod with the given name in the given namespace.
func (r *CheckControllerReconciler) GetPodNodeName(ctx context.Context, nodeName, namespace string) (string, error) {
	pod := &corev1.Pod{}
	err := r.Get(ctx, client.ObjectKey{Name: nodeName, Namespace: namespace}, pod)
	if err != nil {
		return "", fmt.Errorf("error getting pod: %w", err)
	}
	if pod.Spec.NodeName == "" {
		return "", fmt.Errorf("pod has no node name")
	}
	return pod.Spec.NodeName, nil
}

// CreatePod creates a pod on the node with the given name for rebooting the node.
func (r *CheckControllerReconciler) CreateRebooterPod(ctx context.Context, nodeName, podNodeName, namespace string) error {
	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("node-rebooter-%s", nodeName),
			Namespace: namespace,
			Labels: map[string]string{
				"k8s-app": "node-rebooter",
			},
		},
		Spec: corev1.PodSpec{
			NodeSelector: map[string]string{
				"kubernetes.io/hostname": podNodeName,
			},
			Containers: []corev1.Container{
				{
					Name:  "node-rebooter",
					Image: "busybox",
					Command: []string{
						"/bin/sh",
						"-c",
						"--",
					},
					Args: []string{
						"echo 1 > /host_proc/proc/sys/kernel/sysrq; echo b > /host_proc/sysrq-trigger",
					},
					SecurityContext: &corev1.SecurityContext{
						Privileged: ptr.To(true),
					},
					VolumeMounts: []corev1.VolumeMount{
						{
							MountPath: "/host_proc",
							Name:      "proc",
						},
					},
				},
			},
			RestartPolicy: "Never",
			Tolerations: []corev1.Toleration{
				{
					Operator: "Exists",
				},
			},
			Volumes: []corev1.Volume{
				{
					Name: "proc",
					VolumeSource: corev1.VolumeSource{
						HostPath: &corev1.HostPathVolumeSource{
							Path: "/proc",
						},
					},
				},
			},
		},
	}
	err := r.Create(ctx, pod)
	if err != nil {
		return fmt.Errorf("error creating pod: %w", err)
	}
	return nil
}

// GetNode gets the node with the given name.
func (r *CheckControllerReconciler) GetNode(ctx context.Context, nodeName string) (*corev1.Node, error) {
	node := &corev1.Node{}
	err := r.Get(ctx, client.ObjectKey{Name: nodeName}, node)
	if err != nil {
		return nil, fmt.Errorf("error getting node: %w", err)
	}
	return node, nil
}

// DrainNode orchestrates the draining process for the node with the given name.
func (r *CheckControllerReconciler) DrainNode(ctx context.Context, node *corev1.Node) error {
	logger := log.FromContext(ctx).WithName("drainNode")
	logger.V(1).Info("Starting node draining process", "nodeName", node.Name)

	if err := r.MarkNodeUnschedulable(ctx, node); err != nil {
		logger.Error(err, "Failed to mark node as unschedulable", "nodeName", node.Name)
		return err
	}

	if err := r.EvictPodsFromNode(ctx, node.Name); err != nil {
		logger.Error(err, "Failed to evict pods from node", "nodeName", node.Name)
		return err
	}

	logger.V(1).Info("Node successfully drained", "nodeName", node.Name)
	return nil
}

// MarkNodeUnschedulable marks the specified node as unschedulable.
func (r *CheckControllerReconciler) MarkNodeUnschedulable(ctx context.Context, node *corev1.Node) error {
	logger := log.FromContext(ctx).WithName("MarkNodeUnschedulable").WithValues("nodeName", node.Name)
	logger.V(1).Info("Marking node as unschedulable")

	node.Spec.Unschedulable = true
	if err := r.Update(ctx, node); err != nil {
		return fmt.Errorf("failed to update node %s to unschedulable: %w", node.Name, err)
	}
	return nil
}

// EvictPodsFromNode lists and evicts all non-critical pods from the specified node.
func (r *CheckControllerReconciler) EvictPodsFromNode(ctx context.Context, nodeName string) error {
	logger := log.FromContext(ctx).WithName("EvictPodsFromNode").WithValues("nodeName", nodeName)
	logger.V(1).Info("Listing pods on node")

	podList := &corev1.PodList{}
	if err := r.List(ctx, podList, client.MatchingFields{"spec.nodeName": nodeName}); err != nil {
		return fmt.Errorf("failed to list pods on node %s: %w", nodeName, err)
	}

	for _, pod := range podList.Items {
		if ShouldSkipEviction(pod) {
			logger.V(1).Info("Skipping eviction for critical pod", "podName", pod.Name, "priority", pod.Spec.Priority)
			continue
		}

		if err := r.EvictPod(ctx, &pod); err != nil {
			return fmt.Errorf("failed to evict pod %s: %w", pod.Name, err)
		}
	}

	return nil
}

// ShouldSkipEviction determines if a pod should be skipped during eviction.
// Pods with a priority of higher than 1000000 are considered critical and should not be evicted.
func ShouldSkipEviction(pod corev1.Pod) bool {
	return pod.Spec.Priority != nil && *pod.Spec.Priority > 1000000
}

// evictPod evicts the given pod using the Kubernetes eviction API.
func (r *CheckControllerReconciler) EvictPod(ctx context.Context, pod *corev1.Pod) error {
	logger := log.FromContext(ctx).WithName("evictPod")
	logger.V(1).Info("Evicting pod", "podName", pod.Name, "namespace", pod.Namespace)

	eviction := &policyv1.Eviction{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pod.Name,
			Namespace: pod.Namespace,
		},
	}

	if err := r.Client.SubResource("eviction").Create(ctx, pod, eviction); err != nil {
		if apierrors.IsTooManyRequests(err) {
			logger.V(1).Info("Pod eviction is rate-limited, retrying", "podName", pod.Name)
			return nil
		}
		logger.Error(err, "Failed to evict pod", "podName", pod.Name)
		return err
	}

	logger.V(1).Info("Successfully evicted pod", "podName", pod.Name)
	return nil
}

// checkNodeCondition checks if the node with the given name has a custom condition set.
func (r *CheckControllerReconciler) CheckNodeCondition(
	ctx context.Context, node *corev1.Node, nodeConditionType corev1.NodeConditionType, conditionStatus corev1.ConditionStatus,
) (bool, error) {

	for _, condition := range node.Status.Conditions {
		if condition.Type == nodeConditionType && condition.Status == conditionStatus {
			return true, nil
		}
	}

	return false, nil
}

// IsNodeDrained checks if the node with the given name is drained.
func (r *CheckControllerReconciler) IsNodeDrained(node *corev1.Node) bool {
	return node.Spec.Unschedulable
}

// updateKubeNodeConditionSlurm updates the SlurmDown condition on the nodes that are no longer being rebooted.
func (r *CheckControllerReconciler) updateKubeNodeConditionSlurm(ctx context.Context, unschedulableNodes map[string]*corev1.Node) error {
	nodes, err := r.ListNodes(ctx)
	if err != nil {
		return fmt.Errorf("error listing nodes: %w", err)
	}

	for _, n := range nodes {
		if node, ok := unschedulableNodes[n]; ok {
			if err := r.SetNodeConditionIfNotExists(ctx, node, SlurmNodeCondition, corev1.ConditionFalse, "Node is no longer being rebooted"); err != nil {
				return fmt.Errorf("error setting node %s condition %s: %w", node, SlurmNodeCondition, err)
			}
		}
	}
	return nil

}

// ListNodes lists all nodes in the cluster.
func (r *CheckControllerReconciler) ListNodes(ctx context.Context) ([]string, error) {
	nodes := &corev1.NodeList{}
	if err := r.List(ctx, nodes); err != nil {
		return nil, fmt.Errorf("error listing nodes: %w", err)
	}

	var nodeNames []string
	for _, node := range nodes.Items {
		nodeNames = append(nodeNames, node.Name)
	}

	return nodeNames, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *CheckControllerReconciler) SetupWithManager(mgr ctrl.Manager) error {
	ctx := context.Background()

	// Index pods by node name. This is used to list and evict pods from a specific node.
	if err := mgr.GetFieldIndexer().IndexField(ctx, &corev1.Pod{}, "spec.nodeName", func(rawObj client.Object) []string {
		pod := rawObj.(*corev1.Pod)
		return []string{pod.Spec.NodeName}
	}); err != nil {
		return fmt.Errorf("failed to setup field indexer: %w", err)
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&slurmv1.SlurmCluster{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Complete(r)
}
