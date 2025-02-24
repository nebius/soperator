package soperatorchecks

import (
	"context"
	"fmt"
	"maps"
	"time"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
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
	"nebius.ai/slurm-operator/internal/slurmapi"
)

//+kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch;create;delete;update
//+kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch;delete;update
//+kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;update
//+kubebuilder:rbac:groups=core,resources=nodes/status,verbs=get;update;patch;watch;list

var (
	ControllerName = "soperatorchecks"
)

type SoperatorChecksReconciler struct {
	*reconciler.Reconciler
	reconcileTimeout time.Duration

	slurmWorkersController *slurmWorkersController
	k8sNodesController     *k8sNodesController

	// TODO: make configurable
	maxUnavailableNodes int
}

func NewSoperatorChecksReconciler(
	client client.Client,
	scheme *runtime.Scheme,
	recorder record.EventRecorder,
	slurmAPIClients map[types.NamespacedName]slurmapi.Client,
	reconcileTimeout time.Duration,
) *SoperatorChecksReconciler {
	r := reconciler.NewReconciler(client, scheme, recorder)
	return &SoperatorChecksReconciler{
		Reconciler:             r,
		reconcileTimeout:       reconcileTimeout,
		slurmWorkersController: newSlurmWorkersController(client, slurmAPIClients),
		k8sNodesController:     newK8SNodesController(client),
		maxUnavailableNodes:    1,
	}
}

func (r *SoperatorChecksReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName(ControllerName)
	logger.Info(fmt.Sprintf("Reconciling %s", req.Name))

	shouldReconcile, err := r.shouldReconcile(ctx, req)
	if err != nil {
		logger.V(1).Error(err, "Check shouldReconcile produced an error")
		return ctrl.Result{RequeueAfter: r.reconcileTimeout}, err
	}
	if !shouldReconcile {
		logger.Info("Maximum unavailable nodes is reached, skipping reconcilation")
		return ctrl.Result{RequeueAfter: r.reconcileTimeout}, nil
	}

	logger.Info("Running slurm workers controller")
	if err := r.slurmWorkersController.reconcile(ctx, req); err != nil {
		logger.V(1).Error(err, "Reconcile slurm workers controller produced an error")
		return ctrl.Result{RequeueAfter: r.reconcileTimeout}, err
	}

	logger.Info("Running k8s nodes controller")
	if err := r.k8sNodesController.reconcile(ctx, req); err != nil {
		logger.V(1).Error(err, "Reconcile k8s nodes controller produced an error")
		return ctrl.Result{RequeueAfter: r.reconcileTimeout}, err
	}

	return ctrl.Result{RequeueAfter: r.reconcileTimeout}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *SoperatorChecksReconciler) SetupWithManager(mgr ctrl.Manager,
	maxConcurrency int, cacheSyncTimeout time.Duration) error {
	ctx := context.Background()

	// Index pods by node name. This is used to list and evict pods from a specific node.
	if err := mgr.GetFieldIndexer().IndexField(ctx, &corev1.Pod{}, "spec.nodeName", func(rawObj client.Object) []string {
		pod := rawObj.(*corev1.Pod)
		return []string{pod.Spec.NodeName}
	}); err != nil {
		return fmt.Errorf("failed to setup field indexer: %w", err)
	}

	// TODO: common code for predicates
	return ctrl.NewControllerManagedBy(mgr).
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
						case consts.SlurmNodeDrain, consts.SlurmNodeReboot, consts.K8SNodeMaintenanceScheduled, consts.K8SNodeDegraded:
							condition := condition
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

func (r *SoperatorChecksReconciler) shouldReconcile(ctx context.Context, req ctrl.Request) (bool, error) {
	var (
		// pagination for big clusters
		limit     int64 = 64
		nextToken       = ""

		unavailableNodesCount = 0
	)

	for {
		nodes, err := listK8SNodes(ctx, r.Client, limit, nextToken)
		if err != nil {
			return false, fmt.Errorf("list k8s nodes: %w", err)
		}

		for _, node := range nodes.Items {
			for _, cond := range node.Status.Conditions {
				if (cond.Type == consts.SlurmNodeDrain || cond.Type == consts.SlurmNodeReboot) &&
					cond.Status == corev1.ConditionTrue {

					if node.Name == req.Name {
						// Finish node reconcilation in any case
						return true, nil
					}

					unavailableNodesCount++
					break
				}
			}
		}

		if nodes.ListMeta.Continue == "" {
			break
		}
	}

	return unavailableNodesCount < r.maxUnavailableNodes, nil
}

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
		return err
	}

	// The field node.Status.Conditions belongs to the status of the Node resource.
	// In Kubernetes, the status is considered a "system-owned" object and cannot be
	// modified using a regular Update call.
	// Instead, changes to the status must be made using the Status().Update method.
	for i, cond := range node.Status.Conditions {
		if cond.Type == condition.Type {

			if cond.Status == condition.Status && cond.Reason == string(condition.Reason) {
				logger.Info("Node already has condition")
				// TODO: update the LastHeartbeatTime
				return nil
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
		return nil, fmt.Errorf("get node: %w", err)
	}
	return node, nil
}

func listK8SNodes(ctx context.Context, c client.Client, limit int64, nextToken string) (corev1.NodeList, error) {
	nodes := &corev1.NodeList{}
	if err := c.List(ctx, nodes, &client.ListOptions{
		Limit:    limit,
		Continue: nextToken,
	}); err != nil {
		return corev1.NodeList{}, fmt.Errorf("list nodes: %w", err)
	}
	return *nodes, nil
}
