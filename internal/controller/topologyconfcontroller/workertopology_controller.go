package topologyconfcontroller

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/controller/topologyconfcontroller/deferral"
	"nebius.ai/slurm-operator/internal/controller/topologyconfcontroller/topology"
	"nebius.ai/slurm-operator/internal/controllerconfig"
	"nebius.ai/slurm-operator/internal/utils/resourcegetter"
	topologyrefs "nebius.ai/slurm-operator/internal/utils/slurm/topology"
)

const WorkerTopologyReconcilerName = "workertopology"

var defaultInterval = 1 * time.Minute

// Reasons and actions of the K8s events recorded on the SlurmCluster.
const (
	reasonInvalidTopologySpec       = "InvalidTopologySpec"
	reasonInvalidTopologyConfig     = "InvalidTopologyConfig"
	reasonConflictingTopologyLabels = "ConflictingTopologyLabels"
	reasonInvalidNodeTopologyLabels = "InvalidNodeTopologyLabels"
	reasonTopologyReachesNoNode     = "TopologyReachesNoNode"
	reasonReconfigureRequested      = "ReconfigureRequested"
	reasonReconfigureDeferred       = "ReconfigureDeferred"

	actionRenderTopology = "RenderTopology"
	actionReconfigure    = "Reconfigure"

	maxEventSamples = 5
)

// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=slurmclusters,verbs=get;list;watch
// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=nodesets,verbs=get;list;watch
// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=jailedconfigs,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update
// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch

type WorkerTopologyReconciler struct {
	client              client.Client
	scheme              *runtime.Scheme
	namespace           string
	truncEventRecorder  events.EventRecorder
	reconfigureDeferral deferral.Policy
}

func NewWorkerTopologyReconciler(
	client client.Client,
	scheme *runtime.Scheme,
	namespace string,
	eventRecorder events.EventRecorder,
	reconfigureDeferral deferral.Policy,
) *WorkerTopologyReconciler {
	return &WorkerTopologyReconciler{
		client:              client,
		scheme:              scheme,
		namespace:           namespace,
		truncEventRecorder:  NewTruncateEventRecorder(eventRecorder),
		reconfigureDeferral: reconfigureDeferral,
	}
}

func (r *WorkerTopologyReconciler) SetupWithManager(
	mgr ctrl.Manager,
	maxConcurrency int,
	cacheSyncTimeout time.Duration,
) error {
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
			handler.EnqueueRequestsFromMapFunc(r.findSlurmClustersInNamespace),
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
		// Updates carry the reconfigure confirmation sconfigcontroller writes to the status.
		Watches(&v1alpha1.JailedConfig{},
			handler.EnqueueRequestsFromMapFunc(r.findSlurmClustersInNamespace),
			builder.WithPredicates(predicate.Funcs{
				CreateFunc: func(e event.CreateEvent) bool {
					return false
				},
				DeleteFunc: func(e event.DeleteEvent) bool {
					return isTopologyConfigName(e.Object.GetName())
				},
				UpdateFunc: func(e event.UpdateEvent) bool {
					return isTopologyConfigName(e.ObjectNew.GetName())
				},
				GenericFunc: func(e event.GenericEvent) bool {
					return false
				},
			})).
		// Updates of the node labels ConfigMap are where a node's network path changes appear.
		Watches(&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(r.findSlurmClustersInNamespace),
			builder.WithPredicates(predicate.Funcs{
				CreateFunc: func(e event.CreateEvent) bool {
					return e.Object.GetName() == consts.ConfigMapNameTopologyNodeLabels
				},
				DeleteFunc: func(e event.DeleteEvent) bool {
					return isTopologyConfigName(e.Object.GetName())
				},
				UpdateFunc: func(e event.UpdateEvent) bool {
					return e.ObjectNew.GetName() == consts.ConfigMapNameTopologyNodeLabels
				},
				GenericFunc: func(e event.GenericEvent) bool {
					return false
				},
			})).
		WithOptions(controllerconfig.ControllerOptions(maxConcurrency, cacheSyncTimeout)).
		Complete(controllerconfig.NamedReconciler(WorkerTopologyReconcilerName, r))
}

func (r *WorkerTopologyReconciler) findSlurmClustersInNamespace(ctx context.Context, obj client.Object) []reconcile.Request {
	slurmClusters := &slurmv1.SlurmClusterList{}
	if err := r.client.List(ctx, slurmClusters, client.InNamespace(obj.GetNamespace())); err != nil {
		return nil
	}

	var requests []reconcile.Request
	for _, cluster := range slurmClusters.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: types.NamespacedName{Name: cluster.Name, Namespace: cluster.Namespace},
		})
	}
	return requests
}

func (r *WorkerTopologyReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.V(1).Info("Starting reconciliation")

	// Load the SlurmCluster CR
	slurmCluster := &slurmv1.SlurmCluster{}
	if err := r.client.Get(ctx, req.NamespacedName, slurmCluster); err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("get SlurmCluster: %w", err)
	}
	slurmClusterRef := req.NamespacedName
	logger.V(1).Info("Loaded SlurmCluster")

	// Load NodeSet CRs
	nodeSets, err := resourcegetter.ListNodeSetsByClusterRef(ctx, r.client, slurmClusterRef)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("load NodeSets: %w", err)
	}
	logger.V(1).Info("Loaded NodeSets", "count", len(nodeSets))

	// Validate and build the desired topology specifications
	desiredTopoSpecs := r.newTopoSpecs(logger, slurmCluster, nodeSets)
	if len(desiredTopoSpecs) == 0 {
		return ctrl.Result{RequeueAfter: defaultInterval}, nil
	}

	// Load node topology labels
	topologyLabelsByNode, err := r.loadTopologyLabels(ctx)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("load node topology labels: %w", err)
	}
	logger.V(1).Info("Loaded node topology labels", "count", len(topologyLabelsByNode))

	// Validate and build the desired NodeSet topology models
	desiredTopoNodeSets, err := r.newTopoNodeSets(ctx, logger, slurmCluster, nodeSets, topologyLabelsByNode)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("build NodeSet topology models: %w", err)
	}

	// Validate and build the desired Slurm topology
	desiredSlurmTopology, err := topology.NewMultiTopology(desiredTopoSpecs, desiredTopoNodeSets)
	if err != nil {
		reason := reasonInvalidTopologyConfig
		if errors.As(err, &topology.ConflictingPathsError{}) {
			reason = reasonConflictingTopologyLabels
		}
		r.truncEventRecorder.Eventf(slurmCluster, nil, corev1.EventTypeWarning, reason, actionRenderTopology,
			"Topology config is not published: %v", err)
		return ctrl.Result{}, fmt.Errorf("build desired topologies: %w", err)
	}
	r.recordEmptyTopologies(slurmCluster, *desiredSlurmTopology)

	// Load the published topology config and the topology it currently holds
	config, err := r.loadTopologyConfig(ctx, slurmCluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("load topology config: %w", err)
	}
	currentSlurmTopology, err := config.currentTopology(ctx)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("load current slurm topology: %w", err)
	}

	// Calculate the change from the current topology config and decide whether
	// the config should be updated and whether it requires cluster reconfiguration.
	change := desiredSlurmTopology.ChangeFrom(&currentSlurmTopology)
	logger = logger.WithValues("ConfigChange", change)

	// Withdraw a confirmed reconfigure request before publishing anything
	if err := config.withdrawConfirmedReconfigure(ctx, logger); err != nil {
		return ctrl.Result{}, fmt.Errorf("withdraw confirmed reconfigure: %w", err)
	}

	switch change {
	// There are structural changes in the new topology
	// -> publish the topology config and ask to reconfigure ASAP
	case topology.ChangeRestructured:
		if err := config.requestReconfigure(ctx, logger); err != nil {
			return ctrl.Result{}, fmt.Errorf("request reconfigure: %w", err)
		}
		r.truncEventRecorder.Eventf(slurmCluster, nil, corev1.EventTypeNormal, reasonReconfigureRequested, actionReconfigure,
			"Requested a Slurm reconfigure: the topology structure changed")
		if err := config.publishTopology(ctx, desiredSlurmTopology); err != nil {
			return ctrl.Result{}, fmt.Errorf("publish topology config: %w", err)
		}
		return ctrl.Result{RequeueAfter: defaultInterval}, nil

	// There are changes in the topology that affect job performance
	// -> publish the topology config, but defer reconfiguration to give Slurm controller a quiet window.
	case topology.ChangeReordered:
		now := time.Now()
		pending, _ := config.pendingDeferredReconfigure(now)
		wait, err := config.deferReconfigure(ctx, now)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("defer reconfigure: %w", err)
		}
		if err := config.publishTopology(ctx, desiredSlurmTopology); err != nil {
			return ctrl.Result{}, fmt.Errorf("publish topology config: %w", err)
		}
		if wait > 0 {
			logger.V(1).Info("Deferring reconfigure request", "wait", wait)
			if !pending {
				r.truncEventRecorder.Eventf(slurmCluster, nil, corev1.EventTypeNormal, reasonReconfigureDeferred, actionReconfigure,
					"Deferred the Slurm reconfigure for reordered leaf switches or blocks by %s", wait.Round(time.Second))
			}
			return ctrl.Result{RequeueAfter: min(wait, defaultInterval)}, nil
		}
		if err := config.requestReconfigure(ctx, logger); err != nil {
			return ctrl.Result{}, fmt.Errorf("request reconfigure: %w", err)
		}
		r.truncEventRecorder.Eventf(slurmCluster, nil, corev1.EventTypeNormal, reasonReconfigureRequested, actionReconfigure,
			"Requested a Slurm reconfigure: leaf switches or blocks were reordered")
		return ctrl.Result{RequeueAfter: defaultInterval}, nil

	// There are changes in the topology that dynamic worker registration carries without a reconfigure.
	// -> publish the topology config without asking to reconfigure.
	case topology.ChangeRegistered:
		if err := config.publishTopology(ctx, desiredSlurmTopology); err != nil {
			return ctrl.Result{}, fmt.Errorf("publish topology config: %w", err)
		}
		return ctrl.Result{RequeueAfter: defaultInterval}, nil

	// There are no changes in the topology
	// -> don't update the topology config, but handle existing deferred reconfigure requests.
	case topology.ChangeIdentical:
		pending, wait := config.pendingDeferredReconfigure(time.Now())
		if pending {
			if wait > 0 {
				logger.V(1).Info("Deferring reconfigure request", "wait", wait)
				return ctrl.Result{RequeueAfter: min(wait, defaultInterval)}, nil
			}
			if err := config.requestReconfigure(ctx, logger); err != nil {
				return ctrl.Result{}, fmt.Errorf("request reconfigure: %w", err)
			}
			r.truncEventRecorder.Eventf(slurmCluster, nil, corev1.EventTypeNormal, reasonReconfigureRequested, actionReconfigure,
				"Requested a Slurm reconfigure: the deferred reconfigure for reordered leaf switches or blocks is due")
		}
		return ctrl.Result{RequeueAfter: defaultInterval}, nil

	default:
		return ctrl.Result{}, fmt.Errorf("unknown topology config change %q", change)
	}
}

func (r *WorkerTopologyReconciler) newTopoSpecs(
	logger logr.Logger,
	cluster *slurmv1.SlurmCluster,
	nodeSets []v1alpha1.NodeSet,
) []topology.Spec {
	var topoSpecs []topology.Spec
	if cluster.Spec.Topology == nil {
		return topoSpecs
	}
	for _, topo := range cluster.Spec.Topology.Topologies {
		logger := logger.WithValues("Topology", topo.Name)

		topoSpec, err := topology.NewSpec(
			topo.Name,
			topo.Topo.Type,
			topo.ClusterDefault != nil && *topo.ClusterDefault,
			topo.Topo.BlockSizes,
			listedNodeSetNames(nodeSets, topo.Topo.Type, topo.NodeSetRefs),
		)
		if err != nil {
			logger.Error(err, "Skipping invalid topology spec")
			r.truncEventRecorder.Eventf(cluster, nil, corev1.EventTypeWarning, reasonInvalidTopologySpec, actionRenderTopology,
				"Topology %q is left out of topology.yaml: %v", topo.Name, err)
			continue
		}
		topoSpecs = append(topoSpecs, topoSpec)
	}
	return topoSpecs
}

// listedNodeSetNames resolves a topology's nodeSetRefs, NodeSet names, aggregate Slurm NodeSet names
// or "ALL", to the names of the NodeSets the topology lists.
func listedNodeSetNames(nodeSets []v1alpha1.NodeSet, topoType string, refs []string) []string {
	var names []string
	for _, nodeSet := range topologyrefs.ListedNodeSets(nodeSets, topoType, refs) {
		names = append(names, nodeSet.Name)
	}
	return names
}

type topologyLabels map[string]string

func (r *WorkerTopologyReconciler) loadTopologyLabels(ctx context.Context) (map[string]topologyLabels, error) {
	configMap := &corev1.ConfigMap{
		ObjectMeta: ctrl.ObjectMeta{
			Name:      consts.ConfigMapNameTopologyNodeLabels,
			Namespace: r.namespace,
		},
	}
	if err := r.client.Get(ctx, client.ObjectKeyFromObject(configMap), configMap); err != nil {
		return nil, fmt.Errorf("get node topology labels config map in namespace %q: %w", r.namespace, err)
	}

	result := make(map[string]topologyLabels)
	for nodeName, jsonData := range configMap.Data {
		var topology topologyLabels
		if err := json.Unmarshal([]byte(jsonData), &topology); err != nil {
			return nil, fmt.Errorf("parse topology labels for node %q: %w", nodeName, err)
		}
		result[nodeName] = topology
	}

	return result, nil
}

func (r *WorkerTopologyReconciler) newTopoNodeSets(
	ctx context.Context,
	logger logr.Logger,
	cluster *slurmv1.SlurmCluster,
	nodeSets []v1alpha1.NodeSet,
	topoLabelsByNode map[string]topologyLabels,
) ([]topology.NodeSet, error) {
	clusterRef := types.NamespacedName{Namespace: cluster.Namespace, Name: cluster.Name}
	var topoNodeSets []topology.NodeSet
	var invalidLabels []string
	for _, nodeSet := range nodeSets {
		logger := logger.WithValues("NodeSet", nodeSet.Name)

		var topoNetworkPathsByWorker map[string]topology.NetworkPath
		// CPU-only nodes don't have network paths -> skip loading
		if nodeSet.Spec.GPU.Enabled {
			var invalid []string
			var err error
			topoNetworkPathsByWorker, invalid, err = r.newNetworkPaths(ctx, logger, nodeSet, clusterRef, topoLabelsByNode)
			if err != nil {
				return nil, fmt.Errorf("load network paths of NodeSet %q: %w", nodeSet.Name, err)
			}
			invalidLabels = append(invalidLabels, invalid...)
		}

		topoNodeSet, err := topology.NewNodeSet(
			nodeSet.Name,
			uint(nodeSet.Spec.Replicas),
			nodeSet.Spec.Topology.Fabric,
			topoNetworkPathsByWorker,
		)
		if err != nil {
			logger.Error(err, "Skipping invalid NodeSet")
			continue
		}
		topoNodeSets = append(topoNodeSets, topoNodeSet)
	}
	if len(invalidLabels) > 0 {
		r.truncEventRecorder.Eventf(cluster, nil, corev1.EventTypeWarning, reasonInvalidNodeTopologyLabels, actionRenderTopology,
			"Nodes with invalid topology labels are placed under unknown: %s", summarizeSamples(invalidLabels))
	}
	return topoNodeSets, nil
}

func (r *WorkerTopologyReconciler) newNetworkPaths(
	ctx context.Context,
	logger logr.Logger,
	nodeSet v1alpha1.NodeSet,
	clusterRef types.NamespacedName,
	topoLabelsByNode map[string]topologyLabels,
) (paths map[string]topology.NetworkPath, invalidLabels []string, err error) {
	networkPathsByWorker := make(map[string]topology.NetworkPath)

	pods, err := r.loadWorkerPods(ctx, nodeSet, clusterRef)
	if err != nil {
		return nil, nil, err
	}

	for _, pod := range pods {
		nodeName := pod.Spec.NodeName
		logger := logger.WithValues("Pod", pod.Name)
		if ordinal, ok := podOrdinal(pod.Name, nodeSet.Name); !ok || ordinal >= nodeSet.Spec.Replicas {
			logger.V(1).Info("Skipping pod beyond the NodeSet replicas")
			continue
		}
		if nodeName == "" {
			logger.V(1).Info("Skipping unscheduled pod")
			continue
		}
		logger = logger.WithValues("Node", nodeName)
		topoLabels, ok := topoLabelsByNode[nodeName]
		if !ok {
			logger.V(1).Info("Skipping pod without topology labels")
			continue
		}

		netPath, err := parseTopologyLabels(topoLabels)
		if err != nil {
			logger.Error(err, "Skipping pod with invalid topology labels")
			invalidLabels = append(invalidLabels, fmt.Sprintf("node %q: %v", nodeName, err))
			continue
		}
		networkPathsByWorker[pod.Name] = netPath
	}

	return networkPathsByWorker, invalidLabels, nil
}

// recordEmptyTopologies reports events when the Slurm topology is empty (lists no nodes).
func (r *WorkerTopologyReconciler) recordEmptyTopologies(cluster *slurmv1.SlurmCluster, topologies topology.MultiTopology) {
	for _, topo := range topologies {
		if !topo.IsEmpty() {
			continue
		}
		r.truncEventRecorder.Eventf(cluster, nil, corev1.EventTypeWarning, reasonTopologyReachesNoNode, actionRenderTopology,
			"Topology %q has no nodes and is published as a placeholder", topo.Name)
	}
}

// summarizeSamples joins the sorted items, keeping at most maxEventSamples of them.
func summarizeSamples(items []string) string {
	sorted := slices.Clone(items)
	slices.Sort(sorted)
	shown := min(len(sorted), maxEventSamples)
	summary := strings.Join(sorted[:shown], "; ")
	if omitted := len(sorted) - shown; omitted > 0 {
		summary += fmt.Sprintf("; and %d more", omitted)
	}
	return summary
}

func (r *WorkerTopologyReconciler) loadWorkerPods(
	ctx context.Context,
	nodeSet v1alpha1.NodeSet,
	clusterRef types.NamespacedName,
) ([]corev1.Pod, error) {
	list := &corev1.PodList{}
	selector := client.MatchingLabels{consts.LabelNodeSetKey: nodeSet.Name}
	listOpts := []client.ListOption{client.InNamespace(clusterRef.Namespace), selector}
	if err := r.client.List(ctx, list, listOpts...); err != nil {
		return nil, fmt.Errorf("list pods in namespace %q with label selector %v: %w", clusterRef.Namespace, selector, err)
	}
	return list.Items, nil
}

// podOrdinal is the index of a worker pod named "<nodeSet>-<ordinal>".
func podOrdinal(podName, nodeSetName string) (int32, bool) {
	suffix, ok := strings.CutPrefix(podName, nodeSetName+"-")
	if !ok {
		return 0, false
	}
	ordinal, err := strconv.ParseInt(suffix, 10, 32)
	if err != nil || ordinal < 0 || strconv.FormatInt(ordinal, 10) != suffix {
		return 0, false
	}
	return int32(ordinal), true
}

func parseTopologyLabels(topoLabels topologyLabels) (topology.NetworkPath, error) {
	var pathSegments []topology.NetworkPathSegment
	for labelKey, labelVal := range topoLabels {
		suffix, ok := strings.CutPrefix(labelKey, "tier-")
		if !ok {
			return topology.NetworkPath{}, fmt.Errorf("parse %q: expected a \"tier-\" prefix", labelKey)
		}
		tier, err := strconv.ParseUint(suffix, 10, 64)
		if err != nil || strconv.FormatUint(tier, 10) != suffix {
			return topology.NetworkPath{}, fmt.Errorf("parse %q: expected a non-negative integer without leading zeros", labelKey)
		}

		pathSegments = append(pathSegments, topology.NetworkPathSegment{
			Tier: uint(tier),
			Name: labelVal,
		})
	}
	return topology.NewNetworkPath(pathSegments...)
}

// loadTopologyConfig reads the cluster's topology JailedConfig into a topologyConfig; the config
// has no JailedConfig while nothing is published yet.
func (r *WorkerTopologyReconciler) loadTopologyConfig(ctx context.Context, cluster *slurmv1.SlurmCluster) (*topologyConfig, error) {
	config := newTopologyConfig(r.client, r.scheme, cluster, r.reconfigureDeferral)

	jailedConfig := &v1alpha1.JailedConfig{}
	err := r.client.Get(ctx, config.key, jailedConfig)
	switch {
	case apierrors.IsNotFound(err):
	case err != nil:
		return nil, fmt.Errorf("get JailedConfig %q: %w", config.key.Name, err)
	default:
		config.jailedConfig = jailedConfig
	}
	return config, nil
}
