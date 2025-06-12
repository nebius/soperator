package topologyconfcontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

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
	namespace string
}

func NewNodeTopologyReconciler(
	client client.Client, scheme *runtime.Scheme, namespace string) *NodeTopologyReconciler {
	return &NodeTopologyReconciler{
		BaseReconciler: BaseReconciler{
			Client: client,
			Scheme: scheme,
		},
		namespace: namespace,
	}
}

// NodeTopologyReconciler watches Kubernetes nodes via the API server.
//
// Upon detecting a node with a `topologyconf.slurm.nebius.ai/tier-1` label,
// it records the node’s tier information in the `node-topoly-labels` ConfigMap in a
// `nodeName: [tier-x: switchName, ...]` format.
//
// **Example:**
//
// Given the following nodes:
//
// apiVersion: v1
// kind: Node
// metadata:
//
//	labels:
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
//	  topologyconf.slurm.nebius.ai/tier-1: leaf02
//	  topologyconf.slurm.nebius.ai/tier-2: spine01
//	name: nodeD
//
// The resulting node-topoly-labels ConfigMap would contain:
//
// nodeA: [tier-1: leaf00, tier-2: spine00]
// nodeB: [tier-1: leaf00, tier-2: spine00]
// nodeC: [tier-1: leaf01, tier-2: spine01]
// nodeD: [tier-1: leaf02, tier-2: spine01]
func (r *NodeTopologyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName(NodeTopologyReconcilerName)
	logger.Info("Starting reconciliation", "node", req.Name)

	node, err := r.getNode(ctx, req.Name, logger)
	if err != nil {
		return ctrl.Result{}, err
	}

	if !r.shouldProcessNode(node, req.Name, logger) {
		return ctrl.Result{}, nil
	}

	tierData, err := r.extractTierData(node, req.Name, logger)
	if err != nil {
		return ctrl.Result{}, err
	}

	if err := r.updateTopologyConfigMap(ctx, req.Name, tierData, logger); err != nil {
		return ctrl.Result{}, err
	}

	logger.Info("Successfully updated node topology", "node", req.Name)
	return ctrl.Result{}, nil
}

// getNode retrieves the node object from the Kubernetes API server.
func (r *NodeTopologyReconciler) getNode(ctx context.Context, nodeName string, logger logr.Logger) (*corev1.Node, error) {
	node := &corev1.Node{}
	nodeKey := client.ObjectKey{Name: nodeName}

	if err := r.Client.Get(ctx, nodeKey, node); err != nil {
		logger.Error(err, "Failed to get node", "node", nodeName)
		return nil, client.IgnoreNotFound(err)
	}

	return node, nil
}

// shouldProcessNode checks if the node has the required tier-1 label
func (r *NodeTopologyReconciler) shouldProcessNode(node *corev1.Node, nodeName string, logger logr.Logger) bool {
	if _, hasTierLabel := node.Labels[consts.TierOnePrefix]; !hasTierLabel {
		logger.V(1).Info("Node missing tier-1 label, skipping", "node", nodeName)
		return false
	}
	return true
}

// extractTierData extracts tier labels from the node's labels
func (r *NodeTopologyReconciler) extractTierData(node *corev1.Node, nodeName string, logger logr.Logger) (map[string]string, error) {
	tierData := ExtractTierLabels(node.Labels, consts.TopologyLabelPrefix)

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

// updateTopologyConfigMap updates the ConfigMap with the node's tier data
func (r *NodeTopologyReconciler) updateTopologyConfigMap(ctx context.Context, nodeName string, tierData map[string]string, logger logr.Logger) error {
	configMap, err := r.getOrCreateTopologyLabelsConfigMap(ctx, logger)
	if err != nil {
		return err
	}

	tierDataJSON, err := json.Marshal(tierData)
	if err != nil {
		logger.Error(err, "Failed to marshal tier data", "node", nodeName)
		return fmt.Errorf("failed to serialize tier data for node %s: %w", nodeName, err)
	}

	if configMap.Data == nil {
		configMap.Data = make(map[string]string)
	}
	configMap.Data[nodeName] = string(tierDataJSON)

	if err := r.Client.Update(ctx, configMap); err != nil {
		logger.Error(err, "Failed to update ConfigMap", "configMap", configMap.ObjectMeta.Name)
		return fmt.Errorf("failed to update ConfigMap %s/%s: %w", r.namespace, configMap.ObjectMeta.Name, err)
	}

	return nil
}

// getOrCreateTopologyLabelsConfigMap retrieves or creates the ConfigMap used to store node topology information.
func (r *NodeTopologyReconciler) getOrCreateTopologyLabelsConfigMap(ctx context.Context, logger logr.Logger) (*corev1.ConfigMap, error) {
	configMap := &corev1.ConfigMap{
		ObjectMeta: ctrl.ObjectMeta{
			Name:      consts.ConfigMapNameTopologyNodeLabels,
			Namespace: r.namespace,
		},
	}

	if err := r.GetOrCreateConfigMap(ctx, configMap, nil); err != nil {
		logger.Error(err, "Failed to get or create topology ConfigMap")
		return nil, fmt.Errorf("failed to get or create ConfigMap: %w", err)
	}

	return configMap, nil
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
				_, exists := node.Labels[consts.TierOnePrefix]
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

				_, newHasLabel := newNode.Labels[consts.TierOnePrefix]
				_, oldHasLabel := oldNode.Labels[consts.TierOnePrefix]

				return newHasLabel || (oldHasLabel && oldNode.Labels[consts.TierOnePrefix] != newNode.Labels[consts.TierOnePrefix])
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				node, ok := e.Object.(*corev1.Node)
				if !ok {
					return false
				}
				_, exists := node.Labels[consts.TierOnePrefix]
				return exists
			},
		})).
		WithOptions(controllerconfig.ControllerOptions(maxConcurrency, cacheSyncTimeout)).
		Complete(r)
}
