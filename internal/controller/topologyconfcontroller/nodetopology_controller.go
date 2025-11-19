package topologyconfcontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/controllerconfig"
)

var (
	NodeTopologyReconcilerName = "nodeTopologyReconciler"
)

// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch;
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;update;create

type NodeTopologyReconciler struct {
	BaseReconciler
	Namespace           string
	topologyLabelPrefix string
	// https://github.com/kubernetes-sigs/controller-runtime/issues/3044
	APIReader client.Reader // Direct API reader for pagination
}

func NewNodeTopologyReconciler(
	client client.Client, scheme *runtime.Scheme, namespace, topologyLabelPrefix string, apiReader client.Reader) *NodeTopologyReconciler {
	return &NodeTopologyReconciler{
		BaseReconciler: BaseReconciler{
			Client: client,
			Scheme: scheme,
		},
		Namespace:           namespace,
		topologyLabelPrefix: topologyLabelPrefix,
		APIReader:           apiReader,
	}
}

// NodeTopologyReconciler watches Kubernetes nodes via the API server.
//
// Upon detecting a node with a `topologyconf.slurm.nebius.ai/tier-1` label,
// it records the node’s tier information in the `node-topoly-labels` ConfigMap in a
// `nodeName: [tier-x: switchName, ...]` format.
// For the Blackwell architecture (GBX00 racks) `topologyconf.slurm.nebius.ai/tier-1` label
// is considered as NVL domain of the rack. Remaining `tier-*` labels are considered as
// IB topology.
//
// **Example (not considered as a real block topology):**
//
// Given the following nodes:
//
// apiVersion: v1
// kind: Node
// metadata:
//
//	labels:
//	  topologyconf.slurm.nebius.ai/tier-0: nvl0
//	  topologyconf.slurm.nebius.ai/tier-1: leaf00
//	  topologyconf.slurm.nebius.ai/tier-2: spine00
//	name: nodeA
//
// ---
// apiVersion: v1
// kind: Node
// metadata:
//
//	labels:
//	  topologyconf.slurm.nebius.ai/tier-0: nvl0
//	  topologyconf.slurm.nebius.ai/tier-1: leaf00
//	  topologyconf.slurm.nebius.ai/tier-2: spine00
//	name: nodeB
//
// ---
// apiVersion: v1
// kind: Node
// metadata:
//
//	labels:
//	  topologyconf.slurm.nebius.ai/tier-0: nvl1
//	  topologyconf.slurm.nebius.ai/tier-1: leaf01
//	  topologyconf.slurm.nebius.ai/tier-2: spine01
//	name: nodeC
//
// ---
// apiVersion: v1
// kind: Node
// metadata:
//
//	labels:
//	  topologyconf.slurm.nebius.ai/tier-0: nvl2
//	  topologyconf.slurm.nebius.ai/tier-1: leaf02
//	  topologyconf.slurm.nebius.ai/tier-2: spine01
//	name: nodeD
//
// The resulting node-topoly-labels ConfigMap would contain:
//
// nodeA: [tier-0: nvl0, tier-1: leaf00, tier-2: spine00]
// nodeB: [tier-0: nvl0, tier-1: leaf00, tier-2: spine00]
// nodeC: [tier-0: nvl1, tier-1: leaf01, tier-2: spine01]
// nodeD: [tier-0: nvl2, tier-1: leaf02, tier-2: spine01]
func (r *NodeTopologyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName(NodeTopologyReconcilerName)
	logger.Info("Starting reconciliation", "node", req.Name)

	configMap, err := r.GetOrCreateTopologyLabelsConfigMap(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	node, err := r.getNode(ctx, req.Name)
	if err != nil {
		if errors.IsNotFound(err) {
			if err := r.RemoveNodeFromTopologyConfigMap(ctx, req.Name, configMap, logger); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	if !r.shouldProcessNode(node, req.Name, logger) {
		return ctrl.Result{}, nil
	}

	tierData, err := r.extractTierData(node, req.Name, logger)
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := r.UpdateTopologyConfigMap(ctx, req.Name, tierData, configMap); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("Successfully updated node topology", "node", req.Name)
	return ctrl.Result{}, nil
}

// getNode retrieves the node object from the Kubernetes API server.
func (r *NodeTopologyReconciler) getNode(ctx context.Context, nodeName string) (*corev1.Node, error) {
	node := &corev1.Node{}
	nodeKey := client.ObjectKey{Name: nodeName}

	if err := r.Client.Get(ctx, nodeKey, node); err != nil {
		return nil, err
	}

	return node, nil
}

// shouldProcessNode checks if the node has the required tier-1 label
func (r *NodeTopologyReconciler) shouldProcessNode(node *corev1.Node, nodeName string, logger logr.Logger) bool {
	_, hasTierZero := node.Labels[r.tierZeroLabel()]
	_, hasTierOne := node.Labels[r.tierOneLabel()]

	if !hasTierZero && !hasTierOne {
		logger.V(1).Info("Node missing one of tier-0 or tier-1 label, skipping", "node", nodeName)
		return false
	}
	return true
}

// extractTierData extracts tier labels from the node's labels
func (r *NodeTopologyReconciler) extractTierData(node *corev1.Node, nodeName string, logger logr.Logger) (map[string]string, error) {
	tierData := ExtractTierLabels(node.Labels, r.topologyLabelPrefix)

	if len(tierData) == 0 {
		logger.V(1).Info("Node has no tier labels", "node", nodeName)
		return nil, fmt.Errorf("node %s has no tier labels", nodeName)
	}

	return tierData, nil
}

func ExtractTierLabels(k8sNodeLabels map[string]string, topologyLabelPrefix string) map[string]string {
	tierLabels := make(map[string]string)

	for key, value := range k8sNodeLabels {
		if strings.Contains(key, topologyLabelPrefix+"/tier-") {
			tierKey := key[strings.LastIndex(key, "/")+1:]
			tierLabels[tierKey] = value
		}
	}
	return tierLabels
}

// RemoveNodeFromTopologyConfigMap removes the node's tier data from the ConfigMap
func (r *NodeTopologyReconciler) RemoveNodeFromTopologyConfigMap(ctx context.Context, nodeName string, configMap *corev1.ConfigMap, logger logr.Logger) error {
	if configMap.Data == nil {
		return nil // Nothing to remove if Data is nil
	}
	if _, exists := configMap.Data[nodeName]; exists {
		delete(configMap.Data, nodeName)
		if err := r.Client.Update(ctx, configMap); err != nil {
			return fmt.Errorf("update ConfigMap %s/%s: %w", r.Namespace, configMap.ObjectMeta.Name, err)
		}
		return nil
	}
	logger.V(1).Info("Node not found in ConfigMap, nothing to remove", "node", nodeName)
	return nil
}

// updateTopologyConfigMap updates the ConfigMap with the node's tier data
func (r *NodeTopologyReconciler) UpdateTopologyConfigMap(
	ctx context.Context, nodeName string, tierData map[string]string, configMap *corev1.ConfigMap) error {
	tierDataJSON, err := json.Marshal(tierData)
	if err != nil {
		return fmt.Errorf("serialize tier data for node %s: %w", nodeName, err)
	}

	if configMap.Data == nil {
		configMap.Data = make(map[string]string)
	}
	configMap.Data[nodeName] = string(tierDataJSON)

	if err := r.Client.Update(ctx, configMap); err != nil {
		return fmt.Errorf("update ConfigMap %s/%s: %w", r.Namespace, configMap.ObjectMeta.Name, err)
	}

	return nil
}

// GetOrCreateTopologyLabelsConfigMap retrieves or creates the ConfigMap used to store node topology information.
func (r *NodeTopologyReconciler) GetOrCreateTopologyLabelsConfigMap(ctx context.Context) (*corev1.ConfigMap, error) {
	configMap := &corev1.ConfigMap{
		ObjectMeta: ctrl.ObjectMeta{
			Name:      consts.ConfigMapNameTopologyNodeLabels,
			Namespace: r.Namespace,
		},
	}

	err := r.Client.Get(ctx, client.ObjectKeyFromObject(configMap), configMap)
	if err != nil {
		if !errors.IsNotFound(err) {
			return nil, fmt.Errorf("get ConfigMap: %w", err)
		}

		if err := r.initializeConfigMapWithAllNodes(ctx, configMap); err != nil {
			return nil, fmt.Errorf("initialize ConfigMap with all nodes: %w", err)
		}
	}

	return configMap, nil
}

// initializeConfigMapWithAllNodes creates ConfigMap and populates it with all nodes that have tier labels
func (r *NodeTopologyReconciler) initializeConfigMapWithAllNodes(ctx context.Context, configMap *corev1.ConfigMap) error {
	logger := log.FromContext(ctx).WithName(NodeTopologyReconcilerName)

	configMap.Data = make(map[string]string)

	nodeList := &corev1.NodeList{}
	continueToken := ""

	for {
		listOptions := []client.ListOption{
			client.Limit(consts.DefaultLimit),
		}
		if continueToken != "" {
			listOptions = append(listOptions, client.Continue(continueToken))
		}

		// Use APIReader instead of cached client for pagination support
		// https://github.com/kubernetes-sigs/controller-runtime/issues/3044
		if err := r.APIReader.List(ctx, nodeList, listOptions...); err != nil {
			return fmt.Errorf("list nodes: %w", err)
		}

		for _, node := range nodeList.Items {
			if _, hasTierLabel := node.Labels[r.tierOneLabel()]; hasTierLabel {
				tierData := ExtractTierLabels(node.Labels, r.topologyLabelPrefix)
				if len(tierData) > 0 {
					tierDataJSON, err := json.Marshal(tierData)
					if err != nil {
						logger.Error(err, "Failed to serialize tier data for node", "node", node.Name)
						continue
					}
					configMap.Data[node.Name] = string(tierDataJSON)
				}
			}
		}

		continueToken = nodeList.Continue
		if continueToken == "" {
			break
		}
	}
	if err := r.Client.Create(ctx, configMap); err != nil {
		return fmt.Errorf("create ConfigMap: %w", err)
	}

	logger.Info("Initialized ConfigMap with all nodes", "nodesCount", len(configMap.Data))
	return nil
}

func (r *NodeTopologyReconciler) SetupWithManager(mgr ctrl.Manager,
	maxConcurrency int, cacheSyncTimeout time.Duration) error {
	return ctrl.NewControllerManagedBy(mgr).Named(NodeTopologyReconcilerName).
		For(&corev1.Node{}, builder.WithPredicates(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				node, ok := e.Object.(*corev1.Node)
				if !ok {
					return false
				}
				_, exists := node.Labels[r.tierOneLabel()]
				return exists
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				oldNode, ok := e.ObjectOld.(*corev1.Node)
				if !ok {
					return false
				}
				newNode, ok := e.ObjectNew.(*corev1.Node)
				if !ok {
					return false
				}

				_, newHasLabel := newNode.Labels[r.tierOneLabel()]
				_, oldHasLabel := oldNode.Labels[r.tierOneLabel()]

				return newHasLabel || (oldHasLabel && oldNode.Labels[r.tierOneLabel()] != newNode.Labels[r.tierOneLabel()])
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				node, ok := e.Object.(*corev1.Node)
				if !ok {
					return false
				}
				_, exists := node.Labels[r.tierOneLabel()]
				return exists
			},
		})).
		Watches(&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(r.reconcileConfigMapToRequests),
			builder.WithPredicates(predicate.Funcs{
				CreateFunc: func(e event.CreateEvent) bool {
					return false
				},
				UpdateFunc: func(e event.UpdateEvent) bool {
					return false
				},
				DeleteFunc: func(e event.DeleteEvent) bool {
					cm, ok := e.Object.(*corev1.ConfigMap)
					if !ok {
						return false
					}
					return cm.Name == consts.ConfigMapNameTopologyNodeLabels && cm.Namespace == r.Namespace
				},
				GenericFunc: func(e event.GenericEvent) bool {
					return false
				},
			})).
		WithOptions(controllerconfig.ControllerOptions(maxConcurrency, cacheSyncTimeout)).
		Complete(r)
}

// reconcileConfigMapToRequests handles ConfigMap deletion and recreates it
func (r *NodeTopologyReconciler) reconcileConfigMapToRequests(ctx context.Context, obj client.Object) []reconcile.Request {
	cm, ok := obj.(*corev1.ConfigMap)
	if !ok {
		return nil
	}

	if cm.Name == consts.ConfigMapNameTopologyNodeLabels && cm.Namespace == r.Namespace {
		logger := log.FromContext(ctx).WithName(NodeTopologyReconcilerName)
		logger.Info("Topology node labels ConfigMap was deleted, recreating it", "configMap", cm.Name)

		configMap := &corev1.ConfigMap{
			ObjectMeta: ctrl.ObjectMeta{
				Name:      consts.ConfigMapNameTopologyNodeLabels,
				Namespace: r.Namespace,
			},
		}

		if err := r.initializeConfigMapWithAllNodes(ctx, configMap); err != nil {
			logger.Error(err, "Failed to recreate topology node labels ConfigMap")
		} else {
			logger.Info("Successfully recreated topology node labels ConfigMap")
		}
	}

	// Return empty slice since we don't need to trigger any node reconciliation
	return []reconcile.Request{}
}

func (r *NodeTopologyReconciler) tierZeroLabel() string {
	return r.topologyLabelPrefix + consts.TierZeroSuffix
}

func (r *NodeTopologyReconciler) tierOneLabel() string {
	return r.topologyLabelPrefix + consts.TierOneSuffix
}
