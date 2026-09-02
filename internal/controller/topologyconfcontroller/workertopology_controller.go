package topologyconfcontroller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/events"
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

// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=slurmclusters,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;update;create;patch
// +kubebuilder:rbac:groups=apps.kruise.io,resources=statefulsets,verbs=get;list;watch;
// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=jailedconfigs,verbs=get;list;watch;create;patch
// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=nodesets,verbs=get;list;watch

type WorkerTopologyReconciler struct {
	BaseReconciler
	namespace string
	recorder  events.EventRecorder
}

// Event reasons reported against the SlurmCluster whose topology is misconfigured. They exist
// because these situations are otherwise invisible: the config renders, the operator stays healthy,
// and the first symptom is a job that cannot be scheduled.
const (
	reasonTopologyReachesNoNode  = "TopologyReachesNoNode"
	reasonUnresolvedTopologyRef  = "UnresolvedTopologyRef"
	reasonClusterDefaultConflict = "ClusterDefaultConflict"

	// reasonTopologyRendered reports a published change to topology.yaml. Unlike the reasons above
	// it is not a problem report: it exists so that a node moved between topologies leaves a trace
	// outside the ConfigMap, since such a move changes neither the structure fingerprint nor
	// anything else the operator logs.
	reasonTopologyRendered = "TopologyRendered"

	actionRenderTopology = "RenderTopology"
)

// Link represents a connection in the topology
type Link struct {
	FromSwitch string   // switch name
	ToSwitches []string // connected switches (for higher tier switches)
	ToNodes    []string // connected nodes/pods (for lowest tier switches)
}

func NewWorkerTopologyReconciler(
	client client.Client, scheme *runtime.Scheme, namespace string, recorder events.EventRecorder,
) *WorkerTopologyReconciler {
	return &WorkerTopologyReconciler{
		BaseReconciler: BaseReconciler{
			Client: client,
			Scheme: scheme,
		},
		namespace: namespace,
		recorder:  recorder,
	}
}

// recordTopologyIssue reports a topology misconfiguration on the SlurmCluster.
func (r *WorkerTopologyReconciler) recordTopologyIssue(
	slurmCluster *slurmv1.SlurmCluster, reason, note string, args ...any,
) {
	if r.recorder == nil {
		return
	}
	r.recorder.Eventf(slurmCluster, nil, corev1.EventTypeWarning, reason, actionRenderTopology, note, args...)
}

// recordTopologyRendered reports a published topology.yaml change against the SlurmCluster it was
// rendered for, which puts the event in that cluster's namespace under that cluster's name.
func (r *WorkerTopologyReconciler) recordTopologyRendered(slurmCluster *slurmv1.SlurmCluster, change topologyChange) {
	if r.recorder == nil {
		return
	}
	r.recorder.Eventf(slurmCluster, nil, corev1.EventTypeNormal, reasonTopologyRendered, actionRenderTopology,
		"Published topology.yaml: %s", change.Summary)
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

	configKey := consts.ConfigMapKeyTopologyYAML

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
	desiredStructure, err := topologyStructure(desiredTopology)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("calculate topology structure: %w", err)
	}

	existingTopology := existingTopologyConfig.Data[configKey]
	renderedDesiredTopology := renderManagedTopologyConfig(desiredTopology)

	if r.calculateConfigHash(renderedDesiredTopology) == r.calculateConfigHash(existingTopology) {
		logger.Info("Topology config unchanged, skipping update")
		if err := r.ensureJailedConfig(ctx, req.Namespace, topoConfigName, slurmCluster.Name, configKey, desiredStructure); err != nil {
			return ctrl.Result{}, fmt.Errorf("ensure JailedConfig: %w", err)
		}
		return DefaultRequeueResult, nil
	}

	change := summarizeTopologyChange(existingTopology, renderedDesiredTopology)
	// Reported at info level and as an event on purpose. Node membership is left out of the
	// structure fingerprint, so moving nodes between topologies asks for no reconfigure and would
	// otherwise be published without a single line saying so.
	logger.Info("Topology config changed, publishing it",
		"change", change.Summary, "nodes", change.Detail, "structure", desiredStructure)
	r.recordTopologyRendered(slurmCluster, change)

	// The content is published before the request, on purpose: requesting first would let
	// sconfigcontroller reconfigure the old content and satisfy the request against it. Publishing
	// first means the request only ever stands over content already in the ConfigMap.
	if err := r.updateTopologyConfigMap(ctx, req.Namespace, topoConfigName, desiredTopology, configKey); err != nil {
		logger.Error(err, "Update ConfigMap with topology config")
		return ctrl.Result{}, fmt.Errorf("update ConfigMap with topology config: %w", err)
	}

	if err := r.ensureJailedConfig(
		ctx, req.Namespace, topoConfigName, slurmCluster.Name, configKey, desiredStructure,
	); err != nil {
		return ctrl.Result{}, fmt.Errorf("ensure JailedConfig: %w", err)
	}

	logger.Info("Reconciliation completed successfully")
	return DefaultRequeueResult, nil
}

// isClusterReconciliationNeeded reports whether the cluster describes a network topology at all.
// A cluster that declares none is left alone: there is nothing to render, and nothing to fail over.
func (r *WorkerTopologyReconciler) isClusterReconciliationNeeded(slurmCluster *slurmv1.SlurmCluster) bool {
	return len(namedTopologies(slurmCluster)) > 0
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
	specs := namedTopologies(slurmCluster)
	entries := make([]topologyYAMLEntry, 0, len(specs))
	for _, spec := range specs {
		entries = append(entries, emptyTopologyEntry(spec))
	}
	r.markClusterDefault(ctx, slurmCluster, entries)

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

// buildNodeSetTopologyConfig renders the cluster's named topologies into the body of topology.yaml.
func (r *WorkerTopologyReconciler) buildNodeSetTopologyConfig(
	ctx context.Context, namespace string, slurmCluster *slurmv1.SlurmCluster, nodeSetList []v1alpha1.NodeSet,
) (string, error) {
	nodeTopologyCM, err := r.getNodeTopologyLabelsConfigMap(ctx)
	if err != nil {
		return "", fmt.Errorf("get node topology labels config map: %w", err)
	}

	gpuPodsByNode, err := r.collectScheduledGPUPodsByNode(ctx, nodeSetList, slurmCluster.Name, namespace)
	if err != nil {
		return "", fmt.Errorf("collect scheduled GPU pods: %w", err)
	}

	return r.buildMultiTopologyYAML(ctx, slurmCluster, nodeSetList, nodeTopologyCM, gpuPodsByNode)
}

// collectAllNodeNames returns every Slurm node name derived from the NodeSets' replica ranges,
// including powered-down ephemeral nodes that currently have no pod. Stage 1 of topology building
// relies on this complete list to keep topology.yaml stable across pod lifecycle changes.
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

func (r *WorkerTopologyReconciler) updateTopologyConfigMap(ctx context.Context, namespace, resourceName, config, configKey string) error {
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
		// The ConfigMap is owned wholesale, not merged into: any key other than the one being
		// written is a leftover, and leaving one behind would keep a dead config around that reads
		// like the live one.
		existingConfigMap.Data = map[string]string{configKey: renderedConfig}
		if err := r.Client.Update(ctx, existingConfigMap); err != nil {
			return fmt.Errorf("update ConfigMap %s: %w", existingConfigMap.Name, err)
		}
	}

	return nil
}

// ensureJailedConfig ensures the JailedConfig for topology exists and matches the desired state.
// If it doesn't exist, it creates one. If it exists, it updates the spec to match desired.
func (r *WorkerTopologyReconciler) ensureJailedConfig(
	ctx context.Context, namespace, resourceName, clusterName, configKey, structure string,
) error {
	logger := log.FromContext(ctx).WithName(WorkerTopologyReconcilerName)
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

	desired.Spec.UpdateActions = reconcileReconfigureRequest(logger, existing, structure)
	desired.Annotations = map[string]string{
		consts.AnnotationTopologyStructure:   committedStructure(existing, structure),
		consts.AnnotationTopologyAppliedHash: requestedAtAppliedHash(existing, structure),
	}

	existing.Labels = desired.Labels
	// Merged, not replaced: the JailedConfig may carry annotations this controller knows nothing
	// about, and overwriting would drop them on every reconcile.
	if existing.Annotations == nil {
		existing.Annotations = map[string]string{}
	}
	maps.Copy(existing.Annotations, desired.Annotations)
	existing.Spec = desired.Spec

	if err := r.Client.Update(ctx, existing); err != nil {
		return fmt.Errorf("update JailedConfig %s: %w", existing.Name, err)
	}

	return nil
}

// reconcileReconfigureRequest decides whether the topology config should ask sconfigcontroller for
// a `scontrol reconfigure`.
//
// It asks when the set of topologies, their plugins or their block sizes changed, because only a
// re-read teaches slurmctld about those. It keeps asking until a reconfigure is confirmed for the
// current generation, and withdraws the request only then: clearing it a reconcile later would race
// with sconfigcontroller reading the spec, and the reconfigure would be lost.
func reconcileReconfigureRequest(
	logger logr.Logger, existing *v1alpha1.JailedConfig, structure string,
) []v1alpha1.UpdateAction {
	structureChanged := existing.Annotations[consts.AnnotationTopologyStructure] != structure
	if structureChanged {
		logger.Info("Topology structure changed, requesting a Slurm reconfigure",
			"structure", structure)
		return []v1alpha1.UpdateAction{v1alpha1.UpdateActionReconfigure}
	}

	if !hasReconfigureRequest(existing) {
		return []v1alpha1.UpdateAction{}
	}

	if !reconfigureConfirmed(existing) {
		return existing.Spec.UpdateActions
	}

	logger.Info("Slurm reconfigure confirmed, withdrawing the request")
	return []v1alpha1.UpdateAction{}
}

// reconfigureConfirmed reports whether a reconfigure ran for the content the JailedConfig now
// carries.
//
// The applied hash decides it, not Generation. The request lives in the spec while the structure
// lives in an annotation, so a structural change raised against an already-outstanding request
// leaves Generation untouched; a confirmation earned over the previous content would then read as
// confirming the new one, and the request would be withdrawn before slurmctld ever re-read it.
// The applied hash instead moves only when sconfigcontroller writes new content.
func reconfigureConfirmed(jailedConfig *v1alpha1.JailedConfig) bool {
	condition := meta.FindStatusCondition(jailedConfig.Status.Conditions, string(v1alpha1.ReconfigurePerformed))
	if condition == nil || condition.Status != metav1.ConditionTrue {
		return false
	}
	return jailedConfig.Status.AppliedHash !=
		jailedConfig.Annotations[consts.AnnotationTopologyAppliedHash]
}

// requestedAtAppliedHash pins the applied hash the outstanding request was raised against, so
// reconfigureConfirmed can tell "applied since" from "applied before". It is refreshed whenever the
// structure changes and held steady while the request waits.
func requestedAtAppliedHash(existing *v1alpha1.JailedConfig, structure string) string {
	if existing.Annotations[consts.AnnotationTopologyStructure] != structure {
		return existing.Status.AppliedHash
	}
	if !hasReconfigureRequest(existing) || reconfigureConfirmed(existing) {
		return existing.Status.AppliedHash
	}
	return existing.Annotations[consts.AnnotationTopologyAppliedHash]
}

// committedStructure keeps the previously recorded structure while a reconfigure request is
// outstanding. The first reconciliation records the desired structure alongside the new request;
// later reconciliations must preserve it until that request is confirmed.
func committedStructure(existing *v1alpha1.JailedConfig, structure string) string {
	if !hasReconfigureRequest(existing) || reconfigureConfirmed(existing) {
		return structure
	}
	return existing.Annotations[consts.AnnotationTopologyStructure]
}

func hasReconfigureRequest(jailedConfig *v1alpha1.JailedConfig) bool {
	for _, action := range jailedConfig.Spec.UpdateActions {
		if action == v1alpha1.UpdateActionReconfigure {
			return true
		}
	}
	return false
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
