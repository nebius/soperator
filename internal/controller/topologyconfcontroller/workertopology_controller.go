package topologyconfcontroller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
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
	"nebius.ai/slurm-operator/internal/utils/resourcegetter"
)

var (
	WorkerTopologyReconcilerName = "workerTopologyReconciler"
	DefaultRequeueResult         = ctrl.Result{
		RequeueAfter: 1 * time.Minute,
		Requeue:      true,
	}
	TopologyConfigRegex = regexp.MustCompile(`.*-` + consts.ConfigMapNameTopologyConfig + `$`)
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
	logger := log.FromContext(ctx).WithName(WorkerTopologyReconcilerName).WithValues(
		"SlurmCluster", req.Name, "Namespace", req.Namespace,
	)
	logger.Info("Starting reconciliation")

	slurmCluster := &slurmv1.SlurmCluster{}
	if err := r.Client.Get(ctx, req.NamespacedName, slurmCluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("get SlurmCluster %q in namespace %q: %w", req.Name, req.Namespace, err)
	}

	shouldReconcileCluster := isClusterReconciliationNeeded(slurmCluster)

	if !shouldReconcileCluster {
		return DefaultRequeueResult, nil
	}

	logger.V(1).Info("Fetching nodeSetList for SlurmCluster")
	nodeSetList, err := resourcegetter.ListNodeSetsByClusterRef(
		ctx, r.Client, types.NamespacedName{Namespace: req.Namespace, Name: slurmCluster.Name},
	)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("list NodeSets: %w", err)
	}

	logger.V(1).Info("Fetched NodeSets for SlurmCluster", "count", len(nodeSetList))

	existingTopologyConfig, err := r.EnsureWorkerTopologyConfigMap(ctx, req.Namespace, slurmCluster.Name, logger)
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

	existingTopology := existingTopologyConfig.Data[consts.ConfigMapKeyTopologyConfig]

	desiredHash := r.calculateConfigHash(desiredTopology)
	existingHash := r.calculateConfigHash(existingTopology)

	if desiredHash == existingHash {
		logger.Info("Topology config unchanged, skipping update")
		if err := r.ensureJailedConfig(ctx, req.Namespace, slurmCluster.Name); err != nil {
			return ctrl.Result{}, fmt.Errorf("ensure JailedConfig: %w", err)
		}
		return DefaultRequeueResult, nil
	}

	if err := r.updateTopologyConfigMap(ctx, req.Namespace, slurmCluster.Name, desiredTopology); err != nil {
		logger.Error(err, "Update ConfigMap with topology config")
		return ctrl.Result{}, fmt.Errorf("update ConfigMap with topology config: %w", err)
	}

	logger.Info("Reconciliation completed successfully")
	return DefaultRequeueResult, nil
}

// isClusterReconciliationNeeded checks if the SlurmCluster requires topology reconciliation based on its SlurmConfig.TopologyPlugin setting.
func isClusterReconciliationNeeded(slurmCluster *slurmv1.SlurmCluster) bool {
	return slurmCluster.Spec.SlurmConfig.TopologyPlugin == consts.SlurmTopologyTree ||
		slurmCluster.Spec.SlurmConfig.TopologyPlugin == consts.SlurmTopologyBlock
}

func topologyConfigMapName(clusterName string) string {
	return clusterName + "-" + consts.ConfigMapNameTopologyConfig
}

// EnsureWorkerTopologyConfigMap checks if the topology ConfigMap and JailedConfig exist, and creates them if they don't.
func (r *WorkerTopologyReconciler) EnsureWorkerTopologyConfigMap(
	ctx context.Context, namespace string, clusterName string, logger logr.Logger,
) (*corev1.ConfigMap, error) {
	cmName := topologyConfigMapName(clusterName)
	configMapKey := client.ObjectKey{Name: cmName, Namespace: namespace}
	jailedConfigKey := client.ObjectKey{Name: cmName, Namespace: namespace}

	configMap := &corev1.ConfigMap{}
	configMapExists := true
	err := r.Client.Get(ctx, configMapKey, configMap)
	if err != nil {
		if apierrors.IsNotFound(err) {
			configMapExists = false
			logger.Info("Worker topology ConfigMap not found")
		} else {
			return nil, fmt.Errorf("get ConfigMap %s: %w", cmName, err)
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
			return nil, fmt.Errorf("get JailedConfig %s: %w", cmName, err)
		}
	}

	if !configMapExists || !jailedConfigExists {
		logger.Info("Creating missing topology resources",
			"configMapExists", configMapExists,
			"jailedConfigExists", jailedConfigExists)

		if err = r.createDefaultTopologyResources(ctx, namespace, clusterName); err != nil {
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
	ctx context.Context, namespace string, clusterName string,
) error {

	defaultTopology := "SwitchName=root"

	configMap := r.renderTopologyConfigMap(namespace, defaultTopology, clusterName)
	err := r.Client.Create(ctx, configMap)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create ConfigMap %s: %w", configMap.Name, err)
	}

	jailedConfig := r.renderTopologyJailedConfig(namespace, clusterName)
	err = r.Client.Create(ctx, jailedConfig)
	if err != nil && !apierrors.IsAlreadyExists(err) {
		return fmt.Errorf("create JailedConfig %s: %w", jailedConfig.Name, err)
	}

	return nil
}

func (r *WorkerTopologyReconciler) renderTopologyConfigMap(namespace string, config string, clusterName string) *corev1.ConfigMap {
	cmName := topologyConfigMapName(clusterName)
	return &corev1.ConfigMap{
		TypeMeta: ctrl.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "ConfigMap",
		},
		ObjectMeta: ctrl.ObjectMeta{
			Name:      cmName,
			Namespace: namespace,
			Labels: map[string]string{
				consts.LabelInstanceKey: clusterName,
			},
		},
		Data: map[string]string{
			consts.ConfigMapKeyTopologyConfig: config,
		},
	}
}

func (r *WorkerTopologyReconciler) renderTopologyJailedConfig(namespace string, clusterName string) *v1alpha1.JailedConfig {
	cmName := topologyConfigMapName(clusterName)
	return &v1alpha1.JailedConfig{
		TypeMeta: ctrl.TypeMeta{
			APIVersion: v1alpha1.GroupVersion.String(),
			Kind:       "JailedConfig",
		},
		ObjectMeta: ctrl.ObjectMeta{
			Name:      cmName,
			Namespace: namespace,
			Labels: map[string]string{
				consts.LabelInstanceKey:          clusterName,
				consts.LabelJailedAggregationKey: consts.LabelJailedAggregationCommonValue,
			},
		},
		Spec: v1alpha1.JailedConfigSpec{
			ConfigMap: v1alpha1.ConfigMapReference{
				Name: cmName,
			},
			Items: []corev1.KeyToPath{
				{
					Key:  consts.ConfigMapKeyTopologyConfig,
					Path: filepath.Join("/etc/slurm/", consts.ConfigMapKeyTopologyConfig),
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

	pods, err := r.CollectWorkerPods(ctx, nodeSetList, slurmCluster.Name, namespace)
	if err != nil {
		return "", fmt.Errorf("collect worker pods: %w", err)
	}
	podsByNode := r.GroupPodNamesByNode(pods)

	if slurmCluster.Spec.SlurmConfig.TopologyPlugin == consts.SlurmTopologyBlock {
		var blockSize *int
		if slurmCluster.Spec.Topology != nil {
			blockSize = slurmCluster.Spec.Topology.BlockSize
		}
		return r.BuildTopologyBlocks(ctx, blockSize, nodeTopologyCM, podsByNode)
	}

	return r.BuildTopologyConfig(ctx, nodeTopologyCM, podsByNode)
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

// GroupPodNamesByNode organizes pods by their node name.
func (r *WorkerTopologyReconciler) GroupPodNamesByNode(pods []corev1.Pod) map[string][]string {
	podsByNode := make(map[string][]string, len(pods))
	for _, pod := range pods {
		if pod.Spec.NodeName == "" {
			pod.Spec.NodeName = "unknown"
		}
		podsByNode[pod.Spec.NodeName] = append(podsByNode[pod.Spec.NodeName], pod.Name)
	}
	return podsByNode
}

// BuildTopologyBlocks builds topology config.
func (r *WorkerTopologyReconciler) BuildTopologyBlocks(
	ctx context.Context, blockSize *int, topologyNodeLabelsCM *corev1.ConfigMap, podsByNode map[string][]string,
) (string, error) {
	bs := defBlockSize
	if blockSize != nil {
		bs = *blockSize
	}

	labelsByNode, err := r.ParseNodeTopologyLabels(topologyNodeLabelsCM.Data)
	if err != nil {
		return "", fmt.Errorf("deserialize node block topology: %w", err)
	}
	blocks := BuildTopologyBlocks(ctx, labelsByNode, podsByNode)
	config := strings.Join(blocks.RenderConfigLines(), "\n") + "\n"
	config = fmt.Sprintf("%sBlockSizes=%d\n", config, bs)
	return config, nil
}

// BuildTopologyConfig builds topology config.
func (r *WorkerTopologyReconciler) BuildTopologyConfig(
	ctx context.Context, topologyNodeLabelsCM *corev1.ConfigMap, podsByNode map[string][]string,
) (string, error) {
	labelsByNode, err := r.ParseNodeTopologyLabels(topologyNodeLabelsCM.Data)
	if err != nil {
		return "", fmt.Errorf("deserialize node tree topology: %w", err)
	}
	graph := BuildTopologyGraph(ctx, labelsByNode, podsByNode)
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

func (r *WorkerTopologyReconciler) updateTopologyConfigMap(ctx context.Context, namespace, clusterName, config string) error {
	cmName := topologyConfigMapName(clusterName)
	configMapKey := client.ObjectKey{Name: cmName, Namespace: namespace}
	existingConfigMap := &corev1.ConfigMap{}
	err := r.Client.Get(ctx, configMapKey, existingConfigMap)
	if err != nil {
		if apierrors.IsNotFound(err) {
			cm := r.renderTopologyConfigMap(namespace, config, clusterName)
			if err := r.Client.Create(ctx, cm); err != nil {
				return fmt.Errorf("create ConfigMap %s: %w", cmName, err)
			}
		} else {
			return fmt.Errorf("get ConfigMap %s: %w", cmName, err)
		}
	} else {
		if existingConfigMap.Labels == nil {
			existingConfigMap.Labels = make(map[string]string)
		}
		existingConfigMap.Labels[consts.LabelInstanceKey] = clusterName
		if existingConfigMap.Data == nil {
			existingConfigMap.Data = make(map[string]string)
		}
		existingConfigMap.Data[consts.ConfigMapKeyTopologyConfig] = config
		if err := r.Client.Update(ctx, existingConfigMap); err != nil {
			return fmt.Errorf("update ConfigMap %s: %w", existingConfigMap.Name, err)
		}
	}

	if err := r.ensureJailedConfig(ctx, namespace, clusterName); err != nil {
		return fmt.Errorf("ensure JailedConfig: %w", err)
	}

	return nil
}

// ensureJailedConfig ensures the JailedConfig for topology exists and matches the desired state.
// If it doesn't exist, it creates one. If it exists, it updates the spec to match desired.
func (r *WorkerTopologyReconciler) ensureJailedConfig(ctx context.Context, namespace string, clusterName string) error {
	desired := r.renderTopologyJailedConfig(namespace, clusterName)

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
					return TopologyConfigRegex.MatchString(e.Object.GetName())
				},
				UpdateFunc: func(e event.UpdateEvent) bool {
					return false
				},
				GenericFunc: func(e event.GenericEvent) bool {
					return false
				},
			})).
		Watches(&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(r.findSlurmClusterForTopologyConfig),
			builder.WithPredicates(predicate.Funcs{
				CreateFunc: func(e event.CreateEvent) bool {
					return false
				},
				DeleteFunc: func(e event.DeleteEvent) bool {
					labels := e.Object.GetLabels()
					if labels == nil {
						return false
					}
					instance, ok := labels["app.kubernetes.io/instance"]
					if !ok || instance == "" {
						return false
					}
					return TopologyConfigRegex.MatchString(e.Object.GetName())
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

// findSlurmClusterForTopologyConfig maps topology ConfigMap delete events to a single SlurmCluster
// reconcile request based on the app.kubernetes.io/instance label.
func (r *WorkerTopologyReconciler) findSlurmClusterForTopologyConfig(
	ctx context.Context, obj client.Object,
) []reconcile.Request {
	labels := obj.GetLabels()
	if labels == nil {
		return nil
	}
	instance, ok := labels["app.kubernetes.io/instance"]
	if !ok || instance == "" {
		return nil
	}

	return []reconcile.Request{
		{
			NamespacedName: types.NamespacedName{
				Name:      instance,
				Namespace: obj.GetNamespace(),
			},
		},
	}
}
