package topologyconfcontroller

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strconv"
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
	"nebius.ai/slurm-operator/internal/naming"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
)

var (
	WorkerTopologyReconcilerName = "workerTopologyReconciler"
	DefaultRequeueResult         = ctrl.Result{
		RequeueAfter: 3 * time.Minute,
		Requeue:      true,
	}
)

// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=slurmclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;update;create;patch

type WorkerTopologyReconciler struct {
	BaseReconciler
	namespace string
}

// Link represents a connection in the topology
type Link struct {
	FromSwitch string   // switch name
	ToSwitches []string // connected switches (for higher tier switches)
	ToNodes    []string // connected nodes/pods (for lowest tier switches)
}

func NewWorkerTopologyReconciler(
	client client.Client, scheme *runtime.Scheme, namespace string) *WorkerTopologyReconciler {
	return &WorkerTopologyReconciler{
		BaseReconciler: BaseReconciler{
			Client: client,
			Scheme: scheme,
		},
		namespace: namespace,
	}
}

func (r *WorkerTopologyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName(WorkerTopologyReconcilerName)
	logger.Info(
		"Starting reconciliation", "SlurmCluster", req.Name, "Namespace", req.Namespace,
	)

	nodeTopologyConf, err := r.getTopologyConfigMap(ctx)
	if err != nil {
		logger.Error(err, "Failed to get", "configMap", consts.CongigMapNameNodesTopology)
		return DefaultRequeueResult, nil
	}

	logger.Info(
		"Using ConfigMap for node topology",
		"ConfigMapName", nodeTopologyConf.Name,
		"ConfigMapNamespace", nodeTopologyConf.Namespace,
	)

	labelSelector := client.MatchingLabels{consts.LabelComponentKey: "worker"}
	podList, err := r.getPodList(ctx, labelSelector, req.Namespace, logger)
	if err != nil {
		logger.Error(err, "Failed to list pods with label", "labelSelector", labelSelector)
		return DefaultRequeueResult, nil
	}

	if len(podList.Items) == 0 {
		logger.Info("No pods found with label", "labelSelector", labelSelector)
		return DefaultRequeueResult, nil
	}

	podsByNode := r.GetPodByNode(podList.Items)
	if len(podsByNode) == 0 {
		logger.Info("No pods found organized by node", "podsByNode", podsByNode)
		return DefaultRequeueResult, nil
	}
	logger.Info("Pods organized by node", "podsByNode", podsByNode)

	topologyLinks := make([]string, 0)
	if topologyLinks, err = r.BuildTopologyLinks(nodeTopologyConf, podsByNode); err != nil {
		logger.Error(err, "Failed to build topology links")
		return DefaultRequeueResult, nil
	}
	logger.Info("Built topology links", "topologyLinks", topologyLinks)

	if err := r.updateTopologyConfigMap(ctx, req.Name, req.Namespace, topologyLinks); err != nil {
		logger.Error(err, "Failed to update ConfigMap with topology links")
		return DefaultRequeueResult, nil
	}

	logger.Info("Reconciliation completed successfully")
	return DefaultRequeueResult, nil
}

// getPodList retrieves the list of pods in the specified namespace with the given label selector.
func (r *WorkerTopologyReconciler) getPodList(
	ctx context.Context, labelSelector client.MatchingLabels, namespace string, logger logr.Logger,
) (*corev1.PodList, error) {
	podList := &corev1.PodList{}
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
		labelSelector,
	}

	if err := r.Client.List(ctx, podList, listOpts...); err != nil {
		logger.Error(err, "Failed to list pods")
		return podList, err
	}

	return podList, nil
}

// getPodByNode organizes pods by their node name.
func (r *WorkerTopologyReconciler) GetPodByNode(pods []corev1.Pod) map[string][]string {
	podsByNode := make(map[string][]string)
	for _, pod := range pods {
		if pod.Spec.NodeName != "" {
			podsByNode[pod.Spec.NodeName] = append(podsByNode[pod.Spec.NodeName], pod.Name)
		} else {
			podsByNode["root"] = append(podsByNode["root"], pod.Name)
		}
	}
	return podsByNode
}

type NodeTopology map[string]string

// BuildTopologyLinks builds topology links from nodes
func (r *WorkerTopologyReconciler) BuildTopologyLinks(nodeTopologyConf *corev1.ConfigMap, podsByNode map[string][]string) ([]string, error) {
	tierNodes, err := r.DeserializeNodeTopology(nodeTopologyConf.Data)
	if err != nil {
		return nil, fmt.Errorf("failed to deserialize node topology: %w", err)
	}

	tier1Links := r.BuildTier1Links(tierNodes, podsByNode)
	higherTierLinks := r.BuildHigherTierLinks(tierNodes)

	return append(tier1Links, higherTierLinks...), nil
}

// BuildTier1Links builds links for tier-1 switches
// It connects tier-1 switches to the nodes/pods (workers)
// It returns a slice of strings representing the links for tier-1 switches
// ** Example of the result: **
// [
// "SwitchName=tier-1 Nodes=pod1,pod2,pod3",
// "SwitchName=root Nodes=pod4,pod5"
// ]
func (r *WorkerTopologyReconciler) BuildTier1Links(tierNodes map[string]NodeTopology, podsByNode map[string][]string) []string {
	links := make(map[string][]string)

	for node, topology := range tierNodes {
		tier1Switch := topology["tier-1"]
		links[tier1Switch] = append(links[tier1Switch], podsByNode[node]...)
	}

	if podsByNode, ok := podsByNode["root"]; ok {
		links["root"] = append(links["root"], podsByNode...)
	}

	var result []string
	for switchName, nodes := range links {
		sort.Strings(nodes)
		result = append(result, fmt.Sprintf("SwitchName=%s Nodes=%s", switchName, strings.Join(nodes, ",")))
	}

	return result
}

// BuildHigherTierLinks builds links for higher tiers (tier-2 and above)
// Based on the provided node topology
// It's necessary to have at least 2 tiers for connections
// The tier-1 it's always connected to the nodes/pods (workers)
// Also we could have more than 2 tiers
// So we start from tier-2 and go up to maxTier
// If there are no higher tiers, it returns an empty slice
// Returns a slice of strings representing the links for higher tiers
// ** Example of the result: **
// [
// "SwitchName=leaf0 Switches=switch0,switch0",
// "SwitchName=spine0 Switches=leaf0"
// ]
func (r *WorkerTopologyReconciler) BuildHigherTierLinks(tierNodes map[string]NodeTopology) []string {
	if len(tierNodes) == 0 {
		return []string{}
	}

	maxTier := r.FindMaxTier(tierNodes)
	if maxTier < 2 {
		return []string{}
	}

	var result []string

	for currentTier := 2; currentTier <= maxTier; currentTier++ {
		links := r.BuildLinksForTierWithSlices(tierNodes, currentTier)
		result = append(result, links...)
	}

	return result
}

// FindMaxTier finds the maximum tier number in the given node topology
func (r *WorkerTopologyReconciler) FindMaxTier(tierNodes map[string]NodeTopology) int {
	maxTier := 0

	for _, topology := range tierNodes {
		for tierKey := range topology {
			if strings.HasPrefix(tierKey, "tier-") {
				tierNum := r.ExtractTierNumber(tierKey)
				if tierNum > maxTier {
					maxTier = tierNum
				}
			}
		}
	}

	return maxTier
}

// BuildLinksForTierWithSlices builds links for a specific tier using slices
// It connects the current tier devices with the lower tier devices
// It returns a slice of strings representing the links for the current tier
// It expects the tierNodes to have at least 2 tiers (tier-1 and tier-2)
// For example, if the current tier=2, it will connect tier-2 devices with tier-1 devices
// If tier-3 exists, it will connect tier-2 devices with tier-3 devices, and so on
// if tier-4 and tier-3 does not exist, it will not connect them
func (r *WorkerTopologyReconciler) BuildLinksForTierWithSlices(tierNodes map[string]NodeTopology, currentTier int) []string {
	currentTierKey := fmt.Sprintf("tier-%d", currentTier)
	lowerTierKey := fmt.Sprintf("tier-%d", currentTier-1)

	tierConnections := make(map[string][]string)

	for _, topology := range tierNodes {
		currentTierDevice, hasCurrentTier := topology[currentTierKey]
		lowerTierDevice, hasLowerTier := topology[lowerTierKey]

		if hasCurrentTier && hasLowerTier {
			if _, exists := tierConnections[currentTierDevice]; !exists {
				tierConnections[currentTierDevice] = []string{}
			}
			if !slices.Contains(tierConnections[currentTierDevice], lowerTierDevice) {
				tierConnections[currentTierDevice] = append(tierConnections[currentTierDevice], lowerTierDevice)
			}
		}
	}

	var result []string
	for currentTierDevice, lowerTierDevices := range tierConnections {
		if len(lowerTierDevices) > 0 && currentTierDevice != "" {
			sort.Strings(lowerTierDevices)
			switchesStr := strings.Join(lowerTierDevices, ",")
			if switchesStr == "" {
				continue
			}
			linkStr := fmt.Sprintf("SwitchName=%s Switches=%s", currentTierDevice, switchesStr)
			result = append(result, linkStr)
		}
	}

	sort.Strings(result)
	return result
}

func (r *WorkerTopologyReconciler) ExtractTierNumber(tier string) int {
	parts := strings.Split(tier, "-")
	if len(parts) != 2 {
		return 0
	}
	num, err := strconv.Atoi(parts[1])
	if err != nil {
		return 0
	}
	return num
}

func (r *WorkerTopologyReconciler) DeserializeNodeTopology(data map[string]string) (map[string]NodeTopology, error) {
	result := make(map[string]NodeTopology)

	for nodeName, jsonData := range data {
		var topology NodeTopology
		if err := json.Unmarshal([]byte(jsonData), &topology); err != nil {
			return nil, fmt.Errorf("failed to deserialize topology for node %s: %w", nodeName, err)
		}
		result[nodeName] = topology
	}

	return result, nil
}

func (r *WorkerTopologyReconciler) updateTopologyConfigMap(
	ctx context.Context, clusterName, namespace string, topologyLinks []string) error {

	topologyData := ""
	if len(topologyLinks) > 0 {
		topologyData = strings.Join(topologyLinks, "\n")
	}

	configMap := &corev1.ConfigMap{
		TypeMeta: ctrl.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.Version,
			Kind:       "ConfigMap",
		},
		ObjectMeta: ctrl.ObjectMeta{
			Name:      naming.BuildConfigMapTopologyName(clusterName),
			Namespace: namespace,
			Labels: map[string]string{
				consts.LabelSConfigControllerSourceKey: consts.LabelSConfigControllerSourceValue,
			},
			Annotations: map[string]string{
				consts.AnnotationSConfigControllerSourceKey: consts.DefaultSConfigControllerSourcePath,
			},
		},
		Data: map[string]string{
			"topology.conf": topologyData,
		},
	}

	return r.Client.Patch(ctx, configMap, client.Apply,
		client.ForceOwnership, client.FieldOwner(WorkerTopologyReconcilerName))
}

// getOrCreateTopologyConfigMap retrieves or creates the ConfigMap used to store node topology information.
func (r *WorkerTopologyReconciler) getTopologyConfigMap(ctx context.Context) (*corev1.ConfigMap, error) {
	configMap := &corev1.ConfigMap{
		ObjectMeta: ctrl.ObjectMeta{
			Name:      consts.CongigMapNameNodesTopology,
			Namespace: r.namespace,
		},
	}

	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(configMap), configMap); err != nil {
		return configMap, err
	}

	return configMap, nil
}

func (r *WorkerTopologyReconciler) SetupWithManager(mgr ctrl.Manager,
	maxConcurrency int, cacheSyncTimeout time.Duration) error {
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
		WithOptions(controllerconfig.ControllerOptions(maxConcurrency, cacheSyncTimeout)).
		Complete(r)
}
