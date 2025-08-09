package topologyconfcontroller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strconv"
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
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/controllerconfig"
	"nebius.ai/slurm-operator/internal/render/common"

	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"
)

var (
	WorkerTopologyReconcilerName = "workerTopologyReconciler"
	DefaultRequeueResult         = ctrl.Result{
		RequeueAfter: 1 * time.Minute,
		Requeue:      true,
	}
)

// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=slurmclusters,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;update;create;patch
// +kubebuilder:rbac:groups=apps.kruise.io,resources=statefulsets,verbs=get;list;watch;
// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=jailedconfigs,verbs=get;list;watch;create;patch

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

	slurmCluster := &slurmv1.SlurmCluster{}
	if err := r.Client.Get(ctx, req.NamespacedName, slurmCluster); err != nil {
		return ctrl.Result{}, fmt.Errorf("get SlurmCluster %q in namespace %q: %w", req.Name, req.Namespace, err)
	}

	shouldReconcileCluster := isClusterReconciliationNeeded(slurmCluster)

	if !shouldReconcileCluster {
		return DefaultRequeueResult, nil
	}

	existingTopologyConfigMap, err := r.EnsureWorkerTopologyConfigMap(ctx, req.Namespace, slurmCluster.Name, logger)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("ensure worker topology ConfigMap: %w", err)
	}

	nodeTopologyLabelsConfigMap, err := r.getNodeTopologyLabelsConfigMap(ctx, req, logger)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("handle topology ConfigMap: %w", err)
	}

	logger.Info(
		"Using ConfigMap for topology node labels",
		"ConfigMapName", nodeTopologyLabelsConfigMap.Name,
		"ConfigMapNamespace", nodeTopologyLabelsConfigMap.Namespace,
	)

	labelSelector := client.MatchingLabels{consts.LabelComponentKey: consts.ComponentTypeWorker.String()}
	podList, err := r.getPodList(ctx, labelSelector, req.Namespace)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("list pods with label %v: %w", labelSelector, err)
	}

	if len(podList.Items) == 0 {
		logger.Info("No pods found with label", "labelSelector", labelSelector)
		return DefaultRequeueResult, nil
	}

	podsByNode := r.GetPodsByNode(podList.Items)
	logger.Info("Pods organized by node", "podsByNode", podsByNode)

	desiredTopologyConfig, err := r.BuildTopologyConfig(ctx, nodeTopologyLabelsConfigMap, podsByNode)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("build topology config: %w", err)
	}
	logger.Info("Built topology config", "topologyConfig", desiredTopologyConfig)

	existingTopologyConfig := existingTopologyConfigMap.Data[consts.ConfigMapKeyTopologyConfig]
	if r.calculateConfigHash(desiredTopologyConfig) == r.calculateConfigHash(existingTopologyConfig) {
		logger.Info("Topology config unchanged, skipping update")
		return DefaultRequeueResult, nil
	}

	if err := r.updateTopologyConfigMap(ctx, req.Namespace, desiredTopologyConfig); err != nil {
		logger.Error(err, "Update ConfigMap with topology config")
		return ctrl.Result{}, fmt.Errorf("update ConfigMap with topology config: %w", err)
	}

	logger.Info("Reconciliation completed successfully")
	return DefaultRequeueResult, nil
}

func isClusterReconciliationNeeded(slurmCluster *slurmv1.SlurmCluster) bool {
	return slurmCluster.Spec.SlurmConfig.TopologyPlugin == consts.SlurmTopologyTree
}

func (r *WorkerTopologyReconciler) EnsureWorkerTopologyConfigMap(
	ctx context.Context, namespace string, clusterName string, logger logr.Logger,
) (*corev1.ConfigMap, error) {
	configMap := &corev1.ConfigMap{
		ObjectMeta: ctrl.ObjectMeta{
			Name:      consts.ConfigMapNameTopologyConfig,
			Namespace: namespace,
		},
	}

	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(configMap), configMap); err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Worker topology ConfigMap not found, creating with default topology")
			if err = r.CreateDefaultTopologyConfigMap(ctx, namespace, clusterName); err != nil {
				return nil, fmt.Errorf("create default topology config map in namespace %q: %w", namespace, err)
			}
			if err := r.Client.Get(ctx, client.ObjectKeyFromObject(configMap), configMap); err != nil {
				return nil, fmt.Errorf("get config map after creation in namespace %q: %w", namespace, err)
			}
			logger.Info("Created and retrieved default topology ConfigMap", "configMap", configMap.Name, "namespace", configMap.Namespace)
			return configMap, nil
		}
	}

	return configMap, nil
}

func (r *WorkerTopologyReconciler) getNodeTopologyLabelsConfigMap(
	ctx context.Context, req ctrl.Request, logger logr.Logger) (*corev1.ConfigMap, error) {
	configMap := &corev1.ConfigMap{
		ObjectMeta: ctrl.ObjectMeta{
			Name:      consts.ConfigMapNameTopologyNodeLabels,
			Namespace: r.namespace,
		},
	}

	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(configMap), configMap); err != nil {
		return configMap, fmt.Errorf("get node topology labels config map in namespace %q: %w", req.Namespace, err)
	}

	logger.Info("Node topology labels ConfigMap found", "configMap", configMap.Name, "namespace", configMap.Namespace)
	return configMap, nil
}

func (r *WorkerTopologyReconciler) renderTopologyConfigMap(namespace string, config string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta: ctrl.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.String(),
			Kind:       "ConfigMap",
		},
		ObjectMeta: ctrl.ObjectMeta{
			Name:      consts.ConfigMapNameTopologyConfig,
			Namespace: namespace,
		},
		Data: map[string]string{
			consts.ConfigMapKeyTopologyConfig: config,
		},
	}
}

func (r *WorkerTopologyReconciler) renderTopologyJailedConfig(namespace string) *v1alpha1.JailedConfig {
	return &v1alpha1.JailedConfig{
		TypeMeta: ctrl.TypeMeta{
			APIVersion: v1alpha1.GroupVersion.String(),
			Kind:       "JailedConfig",
		},
		ObjectMeta: ctrl.ObjectMeta{
			Name:      consts.ConfigMapNameTopologyConfig,
			Namespace: namespace,
		},
		Spec: v1alpha1.JailedConfigSpec{
			ConfigMap: v1alpha1.ConfigMapReference{
				Name: consts.ConfigMapNameTopologyConfig,
			},
			Items: []corev1.KeyToPath{
				{
					Key:  consts.ConfigMapKeyTopologyConfig,
					Path: filepath.Join("/etc/slurm/", consts.ConfigMapKeyTopologyConfig),
				},
			},
		},
	}
}

// CreateDefaultTopologyConfigMap creates the ConfigMap for topology configuration with default values.
func (r *WorkerTopologyReconciler) CreateDefaultTopologyConfigMap(
	ctx context.Context, namespace, clusterName string,
) error {
	listASTS, err := r.GetStatefulSetsWithFallback(ctx, namespace, clusterName)
	if err != nil {
		return fmt.Errorf("get StatefulSets with fallback: %w", err)
	}

	config := InitializeTopologyConf(listASTS)

	configMap := r.renderTopologyConfigMap(namespace, config)
	err = r.Client.Create(ctx, configMap)
	if err != nil {
		return fmt.Errorf("create ConfigMap %s: %w", configMap.Name, err)
	}

	jailedConfig := r.renderTopologyJailedConfig(namespace)
	err = r.Client.Create(ctx, jailedConfig)
	if err != nil {
		return fmt.Errorf("create JailedConfig %s: %w", jailedConfig.Name, err)
	}

	return nil
}

// GetStatefulSetsWithFallback retrieves StatefulSets for the cluster and creates a fallback
// StatefulSet based on SlurmCluster spec if none are found.
func (r *WorkerTopologyReconciler) GetStatefulSetsWithFallback(
	ctx context.Context, namespace, clusterName string,
) (*kruisev1b1.StatefulSetList, error) {
	listASTS := &kruisev1b1.StatefulSetList{}

	if err := r.getAdvancedSTS(ctx, clusterName, listASTS); err != nil {
		return nil, fmt.Errorf("get advanced stateful sets: %w", err)
	}

	if len(listASTS.Items) == 0 {
		slurmCluster := &slurmv1.SlurmCluster{}
		if err := r.Client.Get(ctx, client.ObjectKey{Name: clusterName, Namespace: namespace}, slurmCluster); err != nil {
			return nil, fmt.Errorf("get SlurmCluster for fallback topology: %w", err)
		}

		fallbackSTS := kruisev1b1.StatefulSet{
			ObjectMeta: ctrl.ObjectMeta{
				Name: "worker",
			},
			Spec: kruisev1b1.StatefulSetSpec{
				Replicas: &slurmCluster.Spec.SlurmNodes.Worker.Size,
			},
		}

		listASTS.Items = []kruisev1b1.StatefulSet{fallbackSTS}
	}

	return listASTS, nil
}

func (r *WorkerTopologyReconciler) getAdvancedSTS(
	ctx context.Context, clusterName string, asts *kruisev1b1.StatefulSetList) error {
	labels := common.RenderLabels(consts.ComponentTypeWorker, clusterName)
	return r.Client.List(ctx, asts, client.MatchingLabels(labels))
}

func InitializeTopologyConf(asts *kruisev1b1.StatefulSetList) string {
	if asts == nil || len(asts.Items) == 0 {
		return ""
	}

	switchName := "SwitchName=unknown"
	var nodes []string

	for _, sts := range asts.Items {
		if sts.Spec.Replicas == nil || *sts.Spec.Replicas <= 0 {
			continue
		}

		for i := 0; i < int(*sts.Spec.Replicas); i++ {
			nodes = append(nodes, sts.Name+"-"+strconv.Itoa(i))
		}
	}

	if len(nodes) == 0 {
		return switchName
	}

	return switchName + " Nodes=" + strings.Join(nodes, ",")
}

// getPodList retrieves the list of pods in the specified namespace with the given label selector.
func (r *WorkerTopologyReconciler) getPodList(
	ctx context.Context, labelSelector client.MatchingLabels, namespace string,
) (*corev1.PodList, error) {
	podList := &corev1.PodList{}
	listOpts := []client.ListOption{
		client.InNamespace(namespace),
		labelSelector,
	}

	if err := r.Client.List(ctx, podList, listOpts...); err != nil {
		return podList, fmt.Errorf("list pods in namespace %s with label selector %v: %w", namespace, labelSelector, err)
	}

	return podList, nil
}

// GetPodsByNode organizes pods by their node name.
func (r *WorkerTopologyReconciler) GetPodsByNode(pods []corev1.Pod) map[string][]string {
	podsByNode := make(map[string][]string, len(pods))
	for _, pod := range pods {
		podsByNode[pod.Spec.NodeName] = append(podsByNode[pod.Spec.NodeName], pod.Name)
	}
	return podsByNode
}

// NodeTopologyLabels represents the labels for a node's topology, e.g.:
//
//	{
//	  "tier-1": "switch1",
//	  "tier-2": "switch2",
//	  "tier-3": "switch3"
//	}
type NodeTopologyLabels map[string]string

// BuildTopologyConfig builds topology config.
func (r *WorkerTopologyReconciler) BuildTopologyConfig(
	ctx context.Context, nodeTopologyLabelsConf *corev1.ConfigMap, podsByNode map[string][]string,
) (string, error) {
	labelsByNode, err := r.ParseNodeTopologyLabels(nodeTopologyLabelsConf.Data)
	if err != nil {
		return "", fmt.Errorf("deserialize node topology: %w", err)
	}
	graph := BuildTopologyGraph(ctx, labelsByNode, podsByNode)
	config := strings.Join(graph.RenderConfigLines(), "\n") + "\n"
	return config, nil
}

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

func (r *WorkerTopologyReconciler) updateTopologyConfigMap(ctx context.Context, namespace string, config string) error {
	configMap := r.renderTopologyConfigMap(namespace, config)
	err := r.Client.Update(ctx, configMap)
	if err != nil {
		return fmt.Errorf("update ConfigMap %s: %w", configMap.Name, err)
	}

	jailedConfig := r.renderTopologyJailedConfig(namespace)

	err = r.Client.Update(ctx, jailedConfig)
	if err != nil {
		return fmt.Errorf("update JailedConfig %s: %w", jailedConfig.Name, err)
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
		WithOptions(controllerconfig.ControllerOptions(maxConcurrency, cacheSyncTimeout)).
		Complete(r)
}
