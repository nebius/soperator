package checkcontroller

import (
	"context"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
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
	ControllerName = "checkcontroller"
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

	// TODO: Add logic in future
	return ctrl.Result{RequeueAfter: r.reconcileTimeout}, nil
}

// GetPodNodeName gets the node name of the pod with the given name in the given namespace.
func (r *CheckControllerReconciler) GetPodNodeName(ctx context.Context, podName, namespace string) (string, error) {
	pod := &corev1.Pod{}
	err := r.Get(ctx, client.ObjectKey{Name: podName, Namespace: namespace}, pod)
	if err != nil {
		return "", fmt.Errorf("error getting pod: %w", err)
	}
	if pod.Spec.NodeName == "" {
		return "", fmt.Errorf("pod has no pod name")
	}
	return pod.Spec.NodeName, nil
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
