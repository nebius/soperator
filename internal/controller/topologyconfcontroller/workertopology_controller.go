package topologyconfcontroller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"k8s.io/apimachinery/pkg/types"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/controllerconfig"
	"nebius.ai/slurm-operator/internal/render/common"
	renderutils "nebius.ai/slurm-operator/internal/render/utils"
	"nebius.ai/slurm-operator/internal/utils/resourcegetter"
)

var (
	WorkerTopologyReconcilerName = "workerTopologyReconciler"
	DefaultRequeueResult         = ctrl.Result{
		RequeueAfter: 1 * time.Minute,
	}
)

const (
	defBlockSize = 16
)

// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=slurmclusters,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;update;create;patch
// +kubebuilder:rbac:groups=apps.kruise.io,resources=statefulsets,verbs=get;list;watch;
// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=jailedconfigs,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=nodesets,verbs=get;list;watch

type WorkerTopologyReconciler struct {
	BaseReconciler
	namespace string
	// multiTopologyEnabled gates rendering spec.topology.topologies into topology.yaml.
	// See consts.EnvEnableMultiTopology.
	multiTopologyEnabled bool
}

// Link represents a connection in the topology
type Link struct {
	FromSwitch string   // switch name
	ToSwitches []string // connected switches (for higher tier switches)
	ToNodes    []string // connected nodes/pods (for lowest tier switches)
}

func NewWorkerTopologyReconciler(
	client client.Client, scheme *runtime.Scheme, namespace string, multiTopologyEnabled bool,
) *WorkerTopologyReconciler {
	return &WorkerTopologyReconciler{
		BaseReconciler: BaseReconciler{
			Client: client,
			Scheme: scheme,
		},
		namespace:            namespace,
		multiTopologyEnabled: multiTopologyEnabled,
	}
}

func (r *WorkerTopologyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName(WorkerTopologyReconcilerName).WithValues(
		"SlurmCluster", req.Name, "Namespace", req.Namespace,
	)
	logger.Info("Starting reconciliation")

	slurmCluster := &slurmv1.SlurmCluster{}
	if err := r.Client.Get(ctx, req.NamespacedName, slurmCluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("get SlurmCluster %q in namespace %q: %w", req.Name, req.Namespace, err)
	}

	shouldReconcileCluster := r.isClusterReconciliationNeeded(slurmCluster)

	if !shouldReconcileCluster {
		return DefaultRequeueResult, nil
	}

	namePrefix, err := resourcegetter.ResolveWorkloadNamePrefix(ctx, r.Client, req.Namespace, slurmCluster.Name)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("resolve workload name prefix: %w", err)
	}
	topoConfigName := resourcegetter.BuildPrefixedName(namePrefix, consts.ConfigMapNameTopologyConfig)

	logger.V(1).Info("Fetching nodeSetList for SlurmCluster")
	nodeSetList, err := resourcegetter.ListNodeSetsByClusterRef(
		ctx, r.Client, types.NamespacedName{Namespace: req.Namespace, Name: slurmCluster.Name},
	)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("list NodeSets: %w", err)
	}

	logger.V(1).Info("Fetched NodeSets for SlurmCluster", "count", len(nodeSetList))

	if err := r.checkMultiTopologyConfigured(slurmCluster); err != nil {
		logger.Error(err, "Refusing to render a topology config")
		return ctrl.Result{}, err
	}

	configKey := r.topologyConfigKey(slurmCluster)

	existingTopologyConfig, err := r.EnsureWorkerTopologyConfigMap(
		ctx, req.Namespace, topoConfigName, slurmCluster.Name, configKey, slurmCluster, logger,
	)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("ensure worker topology ConfigMap: %w", err)
	}

	desiredTopology, err := r.buildNodeSetTopologyConfig(ctx, req.Namespace, slurmCluster, nodeSetList)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("build NodeSet topology config: %w", err)
	}
	if strings.TrimSpace(desiredTopology) == "" {
		logger.Info("No worker topology to apply yet (empty desired topology), preserving existing topology config")
		return DefaultRequeueResult, nil
	}

	existingTopology := existingTopologyConfig.Data[configKey]
	renderedDesiredTopology := renderManagedTopologyConfig(desiredTopology)

	desiredHash := r.calculateConfigHash(renderedDesiredTopology)
	existingHash := r.calculateConfigHash(existingTopology)

	if desiredHash == existingHash {
		logger.Info("Topology config unchanged, skipping update")
		if err := r.ensureJailedConfig(ctx, req.Namespace, topoConfigName, slurmCluster.Name, configKey); err != nil {
			return ctrl.Result{}, fmt.Errorf("ensure JailedConfig: %w", err)
		}
		return DefaultRequeueResult, nil
	}

	if err := r.updateTopologyConfigMap(ctx, req.Namespace, topoConfigName, desiredTopology, slurmCluster.Name, configKey); err != nil {
		logger.Error(err, "Update ConfigMap with topology config")
		return ctrl.Result{}, fmt.Errorf("update ConfigMap with topology config: %w", err)
	}

	logger.Info("Reconciliation completed successfully")
	return DefaultRequeueResult, nil
}

// isClusterReconciliationNeeded reports whether the cluster asks for a network topology at all,
// either through named topologies or through the legacy SlurmConfig.TopologyPlugin. Clusters that
// ask for neither are left alone.
func (r *WorkerTopologyReconciler) isClusterReconciliationNeeded(slurmCluster *slurmv1.SlurmCluster) bool {
	// Named topologies carry a plugin per entry, so slurm.conf TopologyPlugin no longer decides
	// whether there is anything to render.
	if len(namedTopologies(slurmCluster)) > 0 {
		return true
	}
	return slurmCluster.Spec.SlurmConfig.TopologyPlugin == consts.SlurmTopologyTree ||
		slurmCluster.Spec.SlurmConfig.TopologyPlugin == consts.SlurmTopologyBlock
}

// useMultiTopology reports whether topology.yaml is the format in use. The feature flag alone
// decides it: with the flag on, named topologies are the only source of topology config, so a
// cluster that wants topology but defines none is rejected by checkMultiTopologyConfigured rather
// than quietly falling back to topology.conf.
func (r *WorkerTopologyReconciler) useMultiTopology(_ *slurmv1.SlurmCluster) bool {
	return r.multiTopologyEnabled
}

// checkMultiTopologyConfigured rejects a cluster that asks for a topology while multi-topology is
// enabled but defines no named topology. Falling back to topology.conf here would render a file the
// flag says is no longer in use, leaving no signal about why topology.yaml never appeared.
func (r *WorkerTopologyReconciler) checkMultiTopologyConfigured(slurmCluster *slurmv1.SlurmCluster) error {
	if !r.multiTopologyEnabled || len(namedTopologies(slurmCluster)) > 0 {
		return nil
	}
	return fmt.Errorf(
		"%s is enabled but spec.topology.topologies is empty: define the named topologies, "+
			"or set %s=false to keep rendering the legacy %s",
		consts.EnvEnableMultiTopology, consts.EnvEnableMultiTopology, consts.ConfigMapKeyTopologyConfig,
	)
}

// topologyConfigKey returns the single ConfigMap key the cluster's topology is written to. Slurm
// ignores topology.conf whenever topology.yaml exists, so the two are never emitted together.
func (r *WorkerTopologyReconciler) topologyConfigKey(slurmCluster *slurmv1.SlurmCluster) string {
	if r.useMultiTopology(slurmCluster) {
		return consts.ConfigMapKeyTopologyYAML
	}
	return consts.ConfigMapKeyTopologyConfig
}

// EnsureWorkerTopologyConfigMap checks if the topology ConfigMap and JailedConfig exist, and creates them if they don't.
func (r *WorkerTopologyReconciler) EnsureWorkerTopologyConfigMap(
	ctx context.Context, namespace, resourceName, clusterName, configKey string,
	slurmCluster *slurmv1.SlurmCluster, logger logr.Logger,
) (*corev1.ConfigMap, error) {
	configMapKey := client.ObjectKey{Name: resourceName, Namespace: namespace}
	jailedConfigKey := client.ObjectKey{Name: resourceName, Namespace: namespace}

	configMap := &corev1.ConfigMap{}
	configMapExists := true
	err := r.Client.Get(ctx, configMapKey, configMap)
	if err != nil {
		if apierrors.IsNotFound(err) {
			configMapExists = false
			logger.Info("Worker topology ConfigMap not found")
		} else {
			return nil, fmt.Errorf("get ConfigMap %s: %w", resourceName, err)
		}
	}

	jailedConfig := &v1alpha1.JailedConfig{}
	jailedConfigExists := true
	err = r.Client.Get(ctx, jailedConfigKey, jailedConfig)
	if err != nil {
		if apierrors.IsNotFound(err) {
			jailedConfigExists = false
			logger.Info("Worker topology JailedConfig not found")
		} else {
			return nil, fmt.Errorf("get JailedConfig %s: %w", resourceName, err)
		}
	}

	if !configMapExists || !jailedConfigExists {
		logger.Info("Creating missing topology resources",
			"configMapExists", configMapExists,
			"jailedConfigExists", jailedConfigExists)

		if err = r.createDefaultTopologyResources(ctx, namespace, resourceName, clusterName, configKey, slurmCluster); err != nil {
			return nil, fmt.Errorf("create default topology resources in namespace %q: %w", namespace, err)
		}

		if err := r.Client.Get(ctx, configMapKey, configMap); err != nil {
			return nil, fmt.Errorf("get config map after creation in namespace %q: %w", namespace, err)
		}

		logger.Info("Created and retrieved topology resources",
			"configMap", configMap.Name,
			"namespace", configMap.Namespace)
	}

	return configMap, nil
}

// createDefaultTopologyResources creates the default topology ConfigMap and JailedConfig with a basic topology configuration.
func (r *WorkerTopologyReconciler) createDefaultTopologyResources(
	ctx context.Context, namespace, resourceName, clusterName, configKey string, slurmCluster *slurmv1.SlurmCluster,
) error {

	defaultTopology, err := r.defaultTopologyConfig(ctx, slurmCluster)
	if err != nil {
		return fmt.Errorf("render default topology config: %w", err)
	}

	configMap := r.renderTopologyConfigMap(namespace, resourceName, defaultTopology, configKey)
	if err := r.Client.Create(ctx, configMap); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create ConfigMap %s: %w", configMap.Name, err)
	}

	jailedConfig := r.renderTopologyJailedConfig(namespace, resourceName, clusterName, configKey)
	if err := r.Client.Create(ctx, jailedConfig); err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create JailedConfig %s: %w", jailedConfig.Name, err)
	}

	return nil
}

// defaultTopologyConfig is the placeholder stored when the topology resources are first created,
// before any node topology has been discovered.
//
// Every configured topology is emitted with its own plugin but no members, so partitions
// referencing them by name still resolve. This mirrors the single-topology placeholder, which is a
// lone "SwitchName=root" carrying no nodes. Real switches and blocks replace it on the first
// successful build.
func (r *WorkerTopologyReconciler) defaultTopologyConfig(
	ctx context.Context, slurmCluster *slurmv1.SlurmCluster,
) (string, error) {
	if !r.useMultiTopology(slurmCluster) {
		return "SwitchName=" + consts.SlurmTopologyDefaultFabric, nil
	}

	specs := namedTopologies(slurmCluster)
	entries := make([]topologyYAMLEntry, 0, len(specs))
	for _, spec := range specs {
		entry := topologyYAMLEntry{
			Topology:       spec.Name,
			ClusterDefault: ptr.Deref(spec.ClusterDefault, false),
		}
		if spec.Topo.Type == consts.SlurmTopologyTypeBlock {
			entry.Block = &blockTopologyYAML{BlockSizes: spec.Topo.BlockSizes}
		} else {
			entry.Tree = &treeTopologyYAML{
				Switches: []switchYAML{{Switch: consts.SlurmTopologyDefaultFabric}},
			}
		}
		entries = append(entries, entry)
	}
	markClusterDefault(ctx, entries)

	return renderTopologyYAML(entries)
}

func (r *WorkerTopologyReconciler) renderTopologyConfigMap(namespace, resourceName, config, configKey string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta: ctrl.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "ConfigMap",
		},
		ObjectMeta: ctrl.ObjectMeta{
			Name:      resourceName,
			Namespace: namespace,
		},
		Data: map[string]string{
			configKey: renderManagedTopologyConfig(config),
		},
	}
}

func renderManagedTopologyConfig(config string) string {
	return common.WithManagedSlurmConfigWarning(renderutils.NewAsIsConfig(config)).Render()
}

func (r *WorkerTopologyReconciler) renderTopologyJailedConfig(namespace, resourceName, clusterName, configKey string) *v1alpha1.JailedConfig {
	return &v1alpha1.JailedConfig{
		TypeMeta: ctrl.TypeMeta{
			APIVersion: v1alpha1.GroupVersion.String(),
			Kind:       "JailedConfig",
		},
		ObjectMeta: ctrl.ObjectMeta{
			Name:      resourceName,
			Namespace: namespace,
			Labels: map[string]string{
				consts.LabelJailedAggregationKey: consts.LabelJailedAggregationCommonValue,
				consts.LabelInstanceKey:          clusterName,
			},
		},
		Spec: v1alpha1.JailedConfigSpec{
			ConfigMap: v1alpha1.ConfigMapReference{
				Name: resourceName,
			},
			Items: []corev1.KeyToPath{
				{
					Key:  configKey,
					Path: filepath.Join("/etc/slurm/", configKey),
				},
			},
			UpdateActions: []v1alpha1.UpdateAction{},
		},
	}
}

// buildNodeSetTopologyConfig builds the topology config from NodeSets or worker.Size.
func (r *WorkerTopologyReconciler) buildNodeSetTopologyConfig(
	ctx context.Context, namespace string, slurmCluster *slurmv1.SlurmCluster, nodeSetList []v1alpha1.NodeSet,
) (string, error) {
	nodeTopologyCM, err := r.getNodeTopologyLabelsConfigMap(ctx)
	if err != nil {
		return "", fmt.Errorf("get node topology labels config map: %w", err)
	}

	allNodeNames := collectAllNodeNames(nodeSetList)
	fabricByNode := collectFabricByNode(nodeSetList)

	gpuPodsByNode, err := r.collectScheduledGPUPodsByNode(ctx, nodeSetList, slurmCluster.Name, namespace)
	if err != nil {
		return "", fmt.Errorf("collect scheduled GPU pods: %w", err)
	}

	if r.useMultiTopology(slurmCluster) {
		return r.buildMultiTopologyYAML(ctx, slurmCluster, nodeSetList, nodeTopologyCM, gpuPodsByNode)
	}

	if slurmCluster.Spec.SlurmConfig.TopologyPlugin == consts.SlurmTopologyBlock {
		var blockSize *int
		if slurmCluster.Spec.Topology != nil {
			blockSize = slurmCluster.Spec.Topology.BlockSize
		}
		return r.BuildTopologyBlocks(ctx, blockSize, nodeTopologyCM, gpuPodsByNode, allNodeNames, fabricByNode)
	}

	return r.BuildTopologyConfig(ctx, nodeTopologyCM, gpuPodsByNode, allNodeNames, fabricByNode)
}

// collectAllNodeNames returns every Slurm node name derived from the NodeSets' replica ranges,
// including powered-down ephemeral nodes that currently have no pod. Stage 1 of topology building
// relies on this complete list to keep topology.conf stable across pod lifecycle changes.
func collectAllNodeNames(nodeSetList []v1alpha1.NodeSet) []string {
	var names []string
	for _, nodeSet := range nodeSetList {
		for i := int32(0); i < nodeSet.Spec.Replicas; i++ {
			names = append(names, fmt.Sprintf("%s-%d", nodeSet.Name, i))
		}
	}
	return names
}

// collectFabricByNode maps every Slurm node name to the IB fabric of its NodeSet
// (spec.topology.fabric). NodeSets without an explicit fabric are left out of the map; the
// topology builders fall back to the default fabric for those.
func collectFabricByNode(nodeSetList []v1alpha1.NodeSet) map[string]string {
	fabricByNode := make(map[string]string)
	for _, nodeSet := range nodeSetList {
		fabric := nodeSet.Spec.Topology.Fabric
		if fabric == "" {
			continue
		}
		for i := int32(0); i < nodeSet.Spec.Replicas; i++ {
			fabricByNode[fmt.Sprintf("%s-%d", nodeSet.Name, i)] = fabric
		}
	}
	return fabricByNode
}

// collectScheduledGPUPodsByNode returns pods of GPU-enabled NodeSets that are scheduled to a K8s
// node (Spec.NodeName set, not necessarily Running), grouped by K8s node name. These are the only
// pods that get placed onto a real IB switch in stage 2 of topology building.
func (r *WorkerTopologyReconciler) collectScheduledGPUPodsByNode(
	ctx context.Context, nodeSetList []v1alpha1.NodeSet, slurmClusterName, namespace string,
) (map[string][]string, error) {
	var gpuNodeSets []v1alpha1.NodeSet
	for _, nodeSet := range nodeSetList {
		if nodeSet.Spec.GPU.Enabled {
			gpuNodeSets = append(gpuNodeSets, nodeSet)
		}
	}

	pods, err := r.CollectWorkerPods(ctx, gpuNodeSets, slurmClusterName, namespace)
	if err != nil {
		return nil, fmt.Errorf("collect GPU worker pods: %w", err)
	}

	podsByNode := make(map[string][]string)
	for _, pod := range pods {
		if pod.Spec.NodeName == "" {
			continue
		}
		podsByNode[pod.Spec.NodeName] = append(podsByNode[pod.Spec.NodeName], pod.Name)
	}
	return podsByNode, nil
}

// getNodeTopologyLabelsConfigMap retrieves the ConfigMap containing node topology labels, which is used for building the topology config.
func (r *WorkerTopologyReconciler) getNodeTopologyLabelsConfigMap(ctx context.Context) (*corev1.ConfigMap, error) {
	configMap := &corev1.ConfigMap{
		ObjectMeta: ctrl.ObjectMeta{
			Name:      consts.ConfigMapNameTopologyNodeLabels,
			Namespace: r.namespace,
		},
	}
	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(configMap), configMap); err != nil {
		return configMap, fmt.Errorf("get node topology labels config map in namespace %q: %w", r.namespace, err)
	}
	return configMap, nil
}

// CollectWorkerPods retrieves all worker pods for the given SlurmCluster.
func (r *WorkerTopologyReconciler) CollectWorkerPods(
	ctx context.Context, nodeSetList []v1alpha1.NodeSet, slurmClusterName, namespace string,
) ([]corev1.Pod, error) {

	logger := log.FromContext(ctx).WithValues(
		"SlurmCluster", slurmClusterName, "Namespace", namespace,
	)

	var pods []corev1.Pod

	for _, nodeSet := range nodeSetList {
		labelSelector := client.MatchingLabels{consts.LabelNodeSetKey: nodeSet.Name}

		pl, err := r.listPods(ctx, labelSelector, namespace)
		if err != nil {
			return nil, fmt.Errorf("list pods for NodeSet %s: %w", nodeSet.Name, err)
		}
		if len(pl.Items) == 0 {
			logger.Info(
				"No pods found for NodeSet, skipping",
				"NodeSet", nodeSet.Name, "Namespace", namespace,
			)
			continue
		}

		pods = append(pods, pl.Items...)

	}

	return pods, nil
}

// listPods retrieves the list of pods in the specified namespace with the given label selector.
func (r *WorkerTopologyReconciler) listPods(
	ctx context.Context, labelSelector client.MatchingLabels, ns string,
) (*corev1.PodList, error) {
	podList := &corev1.PodList{}
	listOpts := []client.ListOption{
		client.InNamespace(ns),
		labelSelector,
	}

	if err := r.Client.List(ctx, podList, listOpts...); err != nil {
		return podList, fmt.Errorf("list pods in namespace %s with label selector %v: %w", ns, labelSelector, err)
	}

	return podList, nil
}

// BuildTopologyBlocks builds topology/block config.
func (r *WorkerTopologyReconciler) BuildTopologyBlocks(
	ctx context.Context,
	blockSize *int,
	topologyNodeLabelsCM *corev1.ConfigMap,
	gpuPodsByNode map[string][]string,
	allNodeNames []string,
	fabricByNode map[string]string,
) (string, error) {
	bs := defBlockSize
	if blockSize != nil {
		bs = *blockSize
	}

	labelsByNode, err := r.ParseNodeTopologyLabels(topologyNodeLabelsCM.Data)
	if err != nil {
		return "", fmt.Errorf("deserialize node block topology: %w", err)
	}

	blocks := BuildTopologyBlocks(ctx, labelsByNode, gpuPodsByNode, allNodeNames, fabricByNode)

	config := strings.Join(blocks.RenderConfigLines(), "\n") + "\n"
	config = fmt.Sprintf("%sBlockSizes=%d\n", config, bs)

	return config, nil
}

// BuildTopologyConfig builds topology/tree config.
func (r *WorkerTopologyReconciler) BuildTopologyConfig(
	ctx context.Context,
	topologyNodeLabelsCM *corev1.ConfigMap,
	gpuPodsByNode map[string][]string,
	allNodeNames []string,
	fabricByNode map[string]string,
) (string, error) {
	labelsByNode, err := r.ParseNodeTopologyLabels(topologyNodeLabelsCM.Data)
	if err != nil {
		return "", fmt.Errorf("deserialize node tree topology: %w", err)
	}
	graph := BuildTopologyGraph(ctx, labelsByNode, gpuPodsByNode, allNodeNames, fabricByNode)
	config := strings.Join(graph.RenderConfigLines(), "\n") + "\n"
	return config, nil
}

// NodeTopologyLabels represents the labels for a node's topology, e.g.:
//
//	{
//	  "tier-1": "switch1",
//	  "tier-2": "switch2",
//	  "tier-3": "switch3"
//	}
type NodeTopologyLabels map[string]string

func (r *WorkerTopologyReconciler) ParseNodeTopologyLabels(data map[string]string) (map[string]NodeTopologyLabels, error) {
	result := make(map[string]NodeTopologyLabels)

	for nodeName, jsonData := range data {
		var topology NodeTopologyLabels
		if err := json.Unmarshal([]byte(jsonData), &topology); err != nil {
			return nil, fmt.Errorf("parse topology labels for node %s: %w", nodeName, err)
		}
		result[nodeName] = topology
	}

	return result, nil
}

func (r *WorkerTopologyReconciler) calculateConfigHash(config string) string {
	hash := sha256.Sum256([]byte(strings.TrimSpace(config)))
	return hex.EncodeToString(hash[:])
}

func (r *WorkerTopologyReconciler) updateTopologyConfigMap(ctx context.Context, namespace, resourceName, config, clusterName, configKey string) error {
	configMapKey := client.ObjectKey{Name: resourceName, Namespace: namespace}
	renderedConfig := renderManagedTopologyConfig(config)
	existingConfigMap := &corev1.ConfigMap{}
	err := r.Client.Get(ctx, configMapKey, existingConfigMap)
	if err != nil {
		if apierrors.IsNotFound(err) {
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: namespace,
				},
				Data: map[string]string{
					configKey: renderedConfig,
				},
			}
			if err := r.Client.Create(ctx, cm); err != nil {
				return fmt.Errorf("create ConfigMap %s: %w", resourceName, err)
			}
		} else {
			return fmt.Errorf("get ConfigMap %s: %w", resourceName, err)
		}
	} else {
		existingConfigMap.Data[configKey] = renderedConfig
		// Drop what the other mode left behind: a leftover topology.conf next to topology.yaml is
		// dead weight that reads like the active config.
		delete(existingConfigMap.Data, otherTopologyConfigKey(configKey))
		if err := r.Client.Update(ctx, existingConfigMap); err != nil {
			return fmt.Errorf("update ConfigMap %s: %w", existingConfigMap.Name, err)
		}
	}

	if err := r.ensureJailedConfig(ctx, namespace, resourceName, clusterName, configKey); err != nil {
		return fmt.Errorf("ensure JailedConfig: %w", err)
	}

	return nil
}

// ensureJailedConfig ensures the JailedConfig for topology exists and matches the desired state.
// If it doesn't exist, it creates one. If it exists, it updates the spec to match desired.
func (r *WorkerTopologyReconciler) ensureJailedConfig(ctx context.Context, namespace, resourceName, clusterName, configKey string) error {
	desired := r.renderTopologyJailedConfig(namespace, resourceName, clusterName, configKey)

	existing := &v1alpha1.JailedConfig{}
	err := r.Client.Get(ctx, client.ObjectKeyFromObject(desired), existing)
	if err != nil {
		if apierrors.IsNotFound(err) {
			if createErr := r.Client.Create(ctx, desired); createErr != nil {
				return fmt.Errorf("create JailedConfig %s: %w", desired.Name, createErr)
			}
			return nil
		}
		return fmt.Errorf("get JailedConfig %s: %w", desired.Name, err)
	}

	existing.Labels = desired.Labels
	existing.Spec = desired.Spec

	if err := r.Client.Update(ctx, existing); err != nil {
		return fmt.Errorf("update JailedConfig %s: %w", existing.Name, err)
	}

	return nil
}

func (r *WorkerTopologyReconciler) SetupWithManager(mgr ctrl.Manager,
	maxConcurrency int, cacheSyncTimeout time.Duration) error {

	// Matches both the legacy unprefixed name and any cluster-prefixed variant.
	// Intentionally broad: events from other clusters in the same namespace will
	// trigger reconciliation, but the reconciler uses its own cluster-specific
	// ConfigMap name so spurious triggers are harmless.
	isTopologyConfigName := func(name string) bool {
		return name == consts.ConfigMapNameTopologyConfig ||
			strings.HasSuffix(name, "-"+consts.ConfigMapNameTopologyConfig)
	}

	return ctrl.NewControllerManagedBy(mgr).Named(WorkerTopologyReconcilerName).
		For(&slurmv1.SlurmCluster{}, builder.WithPredicates(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				return true
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				return false
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				return false
			},
			GenericFunc: func(e event.GenericEvent) bool {
				return false
			},
		})).
		Watches(&v1alpha1.NodeSet{},
			handler.EnqueueRequestsFromMapFunc(r.findSlurmClusterForNodeSet),
			builder.WithPredicates(predicate.Funcs{
				CreateFunc: func(e event.CreateEvent) bool {
					return true
				},
				DeleteFunc: func(e event.DeleteEvent) bool {
					return true
				},
				UpdateFunc: func(e event.UpdateEvent) bool {
					return true
				},
				GenericFunc: func(e event.GenericEvent) bool {
					return false
				},
			})).
		Watches(&v1alpha1.JailedConfig{},
			handler.EnqueueRequestsFromMapFunc(r.findSlurmClusterForJailedConfig),
			builder.WithPredicates(predicate.Funcs{
				CreateFunc: func(e event.CreateEvent) bool {
					return false
				},
				DeleteFunc: func(e event.DeleteEvent) bool {
					return isTopologyConfigName(e.Object.GetName())
				},
				UpdateFunc: func(e event.UpdateEvent) bool {
					return false
				},
				GenericFunc: func(e event.GenericEvent) bool {
					return false
				},
			})).
		Watches(&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(r.findSlurmClusterForNodeSet),
			builder.WithPredicates(predicate.Funcs{
				CreateFunc: func(e event.CreateEvent) bool {
					return false
				},
				DeleteFunc: func(e event.DeleteEvent) bool {
					return isTopologyConfigName(e.Object.GetName())
				},
				UpdateFunc: func(e event.UpdateEvent) bool {
					return false
				},
				GenericFunc: func(e event.GenericEvent) bool {
					return false
				},
			})).
		WithOptions(controllerconfig.ControllerOptions(maxConcurrency, cacheSyncTimeout)).
		Complete(r)
}

// findSlurmClusterForNodeSet maps NodeSet events to SlurmCluster reconcile requests.
func (r *WorkerTopologyReconciler) findSlurmClusterForNodeSet(
	ctx context.Context, obj client.Object,
) []reconcile.Request {
	slurmClusterList := &slurmv1.SlurmClusterList{}
	if err := r.Client.List(ctx, slurmClusterList, client.InNamespace(obj.GetNamespace())); err != nil {
		return nil
	}

	var requests []reconcile.Request
	for _, cluster := range slurmClusterList.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      cluster.Name,
				Namespace: cluster.Namespace,
			},
		})
	}
	return requests
}

// findSlurmClusterForJailedConfig maps JailedConfig delete events to SlurmCluster reconcile requests.
func (r *WorkerTopologyReconciler) findSlurmClusterForJailedConfig(
	ctx context.Context, obj client.Object,
) []reconcile.Request {
	return r.findSlurmClusterForNodeSet(ctx, obj)
}
