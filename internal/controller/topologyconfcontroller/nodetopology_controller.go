package topologyconfcontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"
	kruisev1alpha1 "github.com/openkruise/kruise-api/apps/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/controllerconfig"
)

var (
	NodeTopologyReconcilerName = "nodeTopologyReconciler"
)

// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch;
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;update;create
// +kubebuilder:rbac:groups=apps.kruise.io,resources=resourcedistributions,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=slurmclusters,verbs=get;list;watch

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
// The resulting ResourceDistribution would distribute a ConfigMap containing:
//
// nodeA: [tier-0: nvl0, tier-1: leaf00, tier-2: spine00]
// nodeB: [tier-0: nvl0, tier-1: leaf00, tier-2: spine00]
// nodeC: [tier-0: nvl1, tier-1: leaf01, tier-2: spine01]
// nodeD: [tier-0: nvl2, tier-1: leaf02, tier-2: spine01]
func (r *NodeTopologyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName(NodeTopologyReconcilerName)
	logger.Info("Starting reconciliation", "node", req.Name)

	resourceDistribution, err := r.GetOrCreateTopologyResourceDistribution(ctx)
	if err != nil {
		return ctrl.Result{}, err
	}

	node, err := r.getNode(ctx, req.Name)
	if err != nil {
		if errors.IsNotFound(err) {
			if err := r.RemoveNodeFromResourceDistribution(ctx, req.Name, resourceDistribution, logger); err != nil {
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

	if err := r.UpdateResourceDistribution(ctx, req.Name, tierData, resourceDistribution); err != nil {
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

// getConfigMapDataFromResourceDistribution extracts the ConfigMap data from the ResourceDistribution
func (r *NodeTopologyReconciler) getConfigMapDataFromResourceDistribution(rd *kruisev1alpha1.ResourceDistribution) (map[string]string, error) {
	if rd.Spec.Resource.Raw == nil {
		return make(map[string]string), nil
	}

	var configMap corev1.ConfigMap
	if err := json.Unmarshal(rd.Spec.Resource.Raw, &configMap); err != nil {
		return nil, fmt.Errorf("unmarshal ConfigMap from ResourceDistribution: %w", err)
	}

	if configMap.Data == nil {
		return make(map[string]string), nil
	}

	return configMap.Data, nil
}

// RemoveNodeFromResourceDistribution removes the node's tier data from the ResourceDistribution
func (r *NodeTopologyReconciler) RemoveNodeFromResourceDistribution(
	ctx context.Context, nodeName string, rd *kruisev1alpha1.ResourceDistribution, logger logr.Logger) error {

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Get the latest version of ResourceDistribution
		latestRd := &kruisev1alpha1.ResourceDistribution{}
		if err := r.Client.Get(ctx, types.NamespacedName{Name: rd.Name}, latestRd); err != nil {
			return fmt.Errorf("get latest ResourceDistribution: %w", err)
		}

		configMapData, err := r.getConfigMapDataFromResourceDistribution(latestRd)
		if err != nil {
			return err
		}

		if _, exists := configMapData[nodeName]; !exists {
			logger.V(1).Info("Node not found in ResourceDistribution, nothing to remove", "node", nodeName)
			return nil
		}

		delete(configMapData, nodeName)

		return r.updateResourceDistributionWithData(ctx, latestRd, configMapData)
	})
}

// UpdateResourceDistribution updates the ResourceDistribution with the node's tier data
func (r *NodeTopologyReconciler) UpdateResourceDistribution(
	ctx context.Context, nodeName string, tierData map[string]string, rd *kruisev1alpha1.ResourceDistribution) error {

	tierDataJSON, err := json.Marshal(tierData)
	if err != nil {
		return fmt.Errorf("serialize tier data for node %s: %w", nodeName, err)
	}

	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		// Get the latest version of ResourceDistribution
		latestRd := &kruisev1alpha1.ResourceDistribution{}
		if err := r.Client.Get(ctx, types.NamespacedName{Name: rd.Name}, latestRd); err != nil {
			return fmt.Errorf("get latest ResourceDistribution: %w", err)
		}

		configMapData, err := r.getConfigMapDataFromResourceDistribution(latestRd)
		if err != nil {
			return err
		}

		configMapData[nodeName] = string(tierDataJSON)

		return r.updateResourceDistributionWithData(ctx, latestRd, configMapData)
	})
}

// updateResourceDistributionWithData updates the ResourceDistribution with new ConfigMap data
func (r *NodeTopologyReconciler) updateResourceDistributionWithData(
	ctx context.Context, rd *kruisev1alpha1.ResourceDistribution, configMapData map[string]string) error {

	// Get target namespaces from SlurmClusters
	targetNamespaces, err := r.getTargetNamespaces(ctx)
	if err != nil {
		return fmt.Errorf("get target namespaces: %w", err)
	}

	// Build the embedded ConfigMap
	configMap := corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: consts.ConfigMapNameTopologyNodeLabels,
		},
		Data: configMapData,
	}

	configMapBytes, err := json.Marshal(configMap)
	if err != nil {
		return fmt.Errorf("marshal ConfigMap: %w", err)
	}

	rd.Spec.Resource = runtime.RawExtension{Raw: configMapBytes}
	rd.Spec.Targets = kruisev1alpha1.ResourceDistributionTargets{
		ExcludedNamespaces:     kruisev1alpha1.ResourceDistributionTargetNamespaces{},
		IncludedNamespaces:     targetNamespaces,
		NamespaceLabelSelector: metav1.LabelSelector{},
	}

	if err := r.Client.Update(ctx, rd); err != nil {
		return fmt.Errorf("update ResourceDistribution %s: %w", rd.Name, err)
	}

	return nil
}

// getTargetNamespaces returns the list of namespaces to distribute the ConfigMap to
func (r *NodeTopologyReconciler) getTargetNamespaces(ctx context.Context) (kruisev1alpha1.ResourceDistributionTargetNamespaces, error) {
	namespaceSet := make(map[string]struct{})

	// Always include the operator namespace
	namespaceSet[r.Namespace] = struct{}{}

	// Get all SlurmCluster resources and add their namespaces
	slurmClusters := &slurmv1.SlurmClusterList{}
	if err := r.Client.List(ctx, slurmClusters); err != nil {
		return kruisev1alpha1.ResourceDistributionTargetNamespaces{}, fmt.Errorf("list SlurmClusters: %w", err)
	}

	for _, cluster := range slurmClusters.Items {
		namespaceSet[cluster.Namespace] = struct{}{}
	}

	// Convert to list
	namespaceList := make([]kruisev1alpha1.ResourceDistributionNamespace, 0, len(namespaceSet))
	for ns := range namespaceSet {
		namespaceList = append(namespaceList, kruisev1alpha1.ResourceDistributionNamespace{
			Name: ns,
		})
	}

	return kruisev1alpha1.ResourceDistributionTargetNamespaces{
		List: namespaceList,
	}, nil
}

// GetOrCreateTopologyResourceDistribution retrieves or creates the ResourceDistribution
func (r *NodeTopologyReconciler) GetOrCreateTopologyResourceDistribution(ctx context.Context) (*kruisev1alpha1.ResourceDistribution, error) {
	rd := &kruisev1alpha1.ResourceDistribution{}
	rdKey := client.ObjectKey{Name: consts.ResourceDistributionNameTopology}

	err := r.Client.Get(ctx, rdKey, rd)
	if err != nil {
		if !errors.IsNotFound(err) {
			return nil, fmt.Errorf("get ResourceDistribution: %w", err)
		}

		if err := r.initializeResourceDistributionWithAllNodes(ctx, rd); err != nil {
			return nil, fmt.Errorf("initialize ResourceDistribution with all nodes: %w", err)
		}
	}

	return rd, nil
}

// initializeResourceDistributionWithAllNodes creates ResourceDistribution and populates it with all nodes that have tier labels
func (r *NodeTopologyReconciler) initializeResourceDistributionWithAllNodes(ctx context.Context, rd *kruisev1alpha1.ResourceDistribution) error {
	logger := log.FromContext(ctx).WithName(NodeTopologyReconcilerName)

	configMapData := make(map[string]string)

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
					configMapData[node.Name] = string(tierDataJSON)
				}
			}
		}

		continueToken = nodeList.Continue
		if continueToken == "" {
			break
		}
	}

	// Get target namespaces from SlurmClusters
	targetNamespaces, err := r.getTargetNamespaces(ctx)
	if err != nil {
		return fmt.Errorf("get target namespaces: %w", err)
	}

	// Build the embedded ConfigMap
	configMap := corev1.ConfigMap{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "v1",
			Kind:       "ConfigMap",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: consts.ConfigMapNameTopologyNodeLabels,
		},
		Data: configMapData,
	}

	configMapBytes, err := json.Marshal(configMap)
	if err != nil {
		return fmt.Errorf("marshal ConfigMap: %w", err)
	}

	rd.ObjectMeta = metav1.ObjectMeta{
		Name: consts.ResourceDistributionNameTopology,
	}
	rd.Spec = kruisev1alpha1.ResourceDistributionSpec{
		Resource: runtime.RawExtension{Raw: configMapBytes},
		Targets: kruisev1alpha1.ResourceDistributionTargets{
			ExcludedNamespaces:     kruisev1alpha1.ResourceDistributionTargetNamespaces{},
			IncludedNamespaces:     targetNamespaces,
			NamespaceLabelSelector: metav1.LabelSelector{},
		},
	}

	if err := r.Client.Create(ctx, rd); err != nil {
		return fmt.Errorf("create ResourceDistribution: %w", err)
	}

	logger.Info("Initialized ResourceDistribution with all nodes", "nodesCount", len(configMapData))
	return nil
}

func (r *NodeTopologyReconciler) SetupWithManager(mgr ctrl.Manager,
	maxConcurrency int, cacheSyncTimeout time.Duration) error {
	// Add runnable to check ResourceDistribution existence after manager starts
	if err := mgr.Add(r); err != nil {
		return fmt.Errorf("failed to add runnable: %w", err)
	}

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
		Watches(&kruisev1alpha1.ResourceDistribution{},
			handler.EnqueueRequestsFromMapFunc(r.reconcileResourceDistributionToRequests),
			builder.WithPredicates(predicate.Funcs{
				CreateFunc: func(e event.CreateEvent) bool {
					return false
				},
				UpdateFunc: func(e event.UpdateEvent) bool {
					return false
				},
				DeleteFunc: func(e event.DeleteEvent) bool {
					rd, ok := e.Object.(*kruisev1alpha1.ResourceDistribution)
					if !ok {
						return false
					}
					return rd.Name == consts.ResourceDistributionNameTopology
				},
				GenericFunc: func(e event.GenericEvent) bool {
					return false
				},
			})).
		Watches(&slurmv1.SlurmCluster{},
			handler.EnqueueRequestsFromMapFunc(r.reconcileSlurmClusterToRequests),
			builder.WithPredicates(predicate.Funcs{
				CreateFunc: func(e event.CreateEvent) bool {
					return true
				},
				UpdateFunc: func(e event.UpdateEvent) bool {
					return false
				},
				DeleteFunc: func(e event.DeleteEvent) bool {
					return true
				},
				GenericFunc: func(e event.GenericEvent) bool {
					return false
				},
			})).
		WithOptions(controllerconfig.ControllerOptions(maxConcurrency, cacheSyncTimeout)).
		Complete(r)
}

// reconcileResourceDistributionToRequests handles ResourceDistribution deletion and recreates it
func (r *NodeTopologyReconciler) reconcileResourceDistributionToRequests(ctx context.Context, obj client.Object) []reconcile.Request {
	rd, ok := obj.(*kruisev1alpha1.ResourceDistribution)
	if !ok {
		return nil
	}

	if rd.Name == consts.ResourceDistributionNameTopology {
		logger := log.FromContext(ctx).WithName(NodeTopologyReconcilerName)
		logger.Info("Topology ResourceDistribution was deleted, recreating it", "resourceDistribution", rd.Name)

		newRd := &kruisev1alpha1.ResourceDistribution{}

		if err := r.initializeResourceDistributionWithAllNodes(ctx, newRd); err != nil {
			logger.Error(err, "Failed to recreate topology ResourceDistribution")
		} else {
			logger.Info("Successfully recreated topology ResourceDistribution")
		}
	}

	// Return empty slice since we don't need to trigger any node reconciliation
	return []reconcile.Request{}
}

// reconcileSlurmClusterToRequests handles SlurmCluster create/delete to update target namespaces
func (r *NodeTopologyReconciler) reconcileSlurmClusterToRequests(ctx context.Context, obj client.Object) []reconcile.Request {
	logger := log.FromContext(ctx).WithName(NodeTopologyReconcilerName)
	logger.Info("SlurmCluster changed, updating ResourceDistribution target namespaces", "cluster", obj.GetName(), "namespace", obj.GetNamespace())

	// Get the ResourceDistribution
	rd := &kruisev1alpha1.ResourceDistribution{}
	rdKey := client.ObjectKey{Name: consts.ResourceDistributionNameTopology}

	if err := r.Client.Get(ctx, rdKey, rd); err != nil {
		if errors.IsNotFound(err) {
			logger.V(1).Info("ResourceDistribution not found, will be created on next reconciliation")
			return []reconcile.Request{}
		}
		logger.Error(err, "Failed to get ResourceDistribution")
		return []reconcile.Request{}
	}

	// Get current ConfigMap data
	configMapData, err := r.getConfigMapDataFromResourceDistribution(rd)
	if err != nil {
		logger.Error(err, "Failed to get ConfigMap data from ResourceDistribution")
		return []reconcile.Request{}
	}

	// Update target namespaces
	if err := r.updateResourceDistributionWithData(ctx, rd, configMapData); err != nil {
		logger.Error(err, "Failed to update ResourceDistribution target namespaces")
	} else {
		logger.Info("Successfully updated ResourceDistribution target namespaces")
	}

	// Return empty slice since we don't need to trigger any node reconciliation
	return []reconcile.Request{}
}

// Start is called by the manager when the controller starts
// It checks if ResourceDistribution exists and creates it if not
func (r *NodeTopologyReconciler) Start(ctx context.Context) error {
	logger := log.FromContext(ctx).WithName(NodeTopologyReconcilerName)
	logger.Info(fmt.Sprintf("Starting %s runnable to ensure ResourceDistribution existence", NodeTopologyReconcilerName))

	rd := &kruisev1alpha1.ResourceDistribution{}
	err := r.Client.Get(ctx, client.ObjectKey{
		Name: consts.ResourceDistributionNameTopology,
	}, rd)

	if err == nil {
		return nil
	}

	if !errors.IsNotFound(err) {
		return fmt.Errorf("check ResourceDistribution existence: %w", err)
	}

	logger.Info("ResourceDistribution does not exist, creating it")
	if _, err := r.GetOrCreateTopologyResourceDistribution(ctx); err != nil {
		return fmt.Errorf("create ResourceDistribution: %w", err)
	}

	return nil
}

func (r *NodeTopologyReconciler) tierZeroLabel() string {
	return r.topologyLabelPrefix + consts.TierZeroSuffix
}

func (r *NodeTopologyReconciler) tierOneLabel() string {
	return r.topologyLabelPrefix + consts.TierOneSuffix
}

// Legacy methods for backward compatibility with tests

// GetOrCreateTopologyLabelsConfigMap is deprecated, use GetOrCreateTopologyResourceDistribution
func (r *NodeTopologyReconciler) GetOrCreateTopologyLabelsConfigMap(ctx context.Context) (*corev1.ConfigMap, error) {
	rd, err := r.GetOrCreateTopologyResourceDistribution(ctx)
	if err != nil {
		return nil, err
	}

	configMapData, err := r.getConfigMapDataFromResourceDistribution(rd)
	if err != nil {
		return nil, err
	}

	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      consts.ConfigMapNameTopologyNodeLabels,
			Namespace: r.Namespace,
		},
		Data: configMapData,
	}, nil
}

// UpdateTopologyConfigMap is deprecated, use UpdateResourceDistribution
func (r *NodeTopologyReconciler) UpdateTopologyConfigMap(
	ctx context.Context, nodeName string, tierData map[string]string) error {

	rd, err := r.GetOrCreateTopologyResourceDistribution(ctx)
	if err != nil {
		return err
	}

	return r.UpdateResourceDistribution(ctx, nodeName, tierData, rd)
}

// RemoveNodeFromTopologyConfigMap is deprecated, use RemoveNodeFromResourceDistribution
func (r *NodeTopologyReconciler) RemoveNodeFromTopologyConfigMap(
	ctx context.Context, nodeName string, logger logr.Logger) error {

	rd, err := r.GetOrCreateTopologyResourceDistribution(ctx)
	if err != nil {
		return err
	}

	return r.RemoveNodeFromResourceDistribution(ctx, nodeName, rd, logger)
}
