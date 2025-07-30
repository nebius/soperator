package topologyconfcontroller

import (
	"context"
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
// +kubebuilder:rbac:groups=apps.kruise.io,resources=statefulsets,verbs=get;list;watch;create;update;patch

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
		logger.Error(err, "Get SlurmCluster", "SlurmCluster", req.Name, "Namespace", req.Namespace)
		return DefaultRequeueResult, nil
	}

	shouldReconcileCluster := isClusterReconciliationNeeded(slurmCluster)

	if !shouldReconcileCluster {
		return DefaultRequeueResult, nil
	}

	if err := r.EnsureTopologyConfigMap(ctx, req.Namespace, slurmCluster.Name); err != nil {
		logger.Error(err, "Ensure topology ConfigMap")
		return DefaultRequeueResult, nil
	}

	topologyLabelsConfigMap, err := r.handleTopologyConfigMapFunctional(ctx, req, slurmCluster, logger)
	if err != nil {
		logger.Error(err, "Warnming: cannot handle topology ConfigMap")
		return DefaultRequeueResult, nil
	}

	logger.Info(
		"Using ConfigMap for topology node labels",
		"ConfigMapName", topologyLabelsConfigMap.Name,
		"ConfigMapNamespace", topologyLabelsConfigMap.Namespace,
	)

	labelSelector := client.MatchingLabels{consts.LabelComponentKey: consts.ComponentTypeWorker.String()}
	podList, err := r.getPodList(ctx, labelSelector, req.Namespace)
	if err != nil {
		logger.Error(err, "list pods with label", "labelSelector", labelSelector)
		return DefaultRequeueResult, nil
	}

	if len(podList.Items) == 0 {
		logger.Info("No pods found with label", "labelSelector", labelSelector)
		return DefaultRequeueResult, nil
	}

	podsByNode := r.GetPodsByNode(podList.Items)
	logger.Info("Pods organized by node", "podsByNode", podsByNode)

	topologyConfig, err := r.BuildTopologyConfig(ctx, topologyLabelsConfigMap, podsByNode)
	if err != nil {
		logger.Error(err, "build topology config")
		return DefaultRequeueResult, nil
	}
	logger.Info("Built topology config", "topologyConfig", topologyConfig)

	if err := r.updateTopologyConfigMap(ctx, req.Namespace, topologyConfig); err != nil {
		logger.Error(err, "Update ConfigMap with topology config")
		return DefaultRequeueResult, nil
	}

	logger.Info("Reconciliation completed successfully")
	return DefaultRequeueResult, nil
}

func isClusterReconciliationNeeded(slurmCluster *slurmv1.SlurmCluster) bool {
	return slurmCluster.Spec.SlurmConfig.TopologyPlugin == consts.SlurmTopologyTree
}

func (r *WorkerTopologyReconciler) handleTopologyConfigMapFunctional(
	ctx context.Context, req ctrl.Request, slurmCluster *slurmv1.SlurmCluster, logger logr.Logger) (*corev1.ConfigMap, error) {
	topologyLabelsConfigMap, err := r.getNodeTopologyLabelsConfigMap(ctx)
	if err != nil {
		if apierrors.IsNotFound(err) {
			logger.Info("Node topology labels ConfigMap not found, creating with default topology")
			if err = r.createDefaultTopologyConfigMap(ctx, req, slurmCluster, logger); err != nil {
				return nil, fmt.Errorf("create default topology config map: %w", err)
			}
			return nil, fmt.Errorf("config map %s not found, created with default topology", err)
		}

		return nil, fmt.Errorf("get node topology labels config map: %w", err)
	}

	logger.Info("Node topology labels ConfigMap found", "configMap", topologyLabelsConfigMap.Name)
	return topologyLabelsConfigMap, nil
}

func (r *WorkerTopologyReconciler) renderTopologyConfigMap(namespace string, config string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		TypeMeta: ctrl.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.Version,
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
			APIVersion: v1alpha1.GroupVersion.Version,
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

func (r *WorkerTopologyReconciler) makeDefaultTopologyConfig(ctx context.Context, clusterName string) (string, error) {
	listASTS := &kruisev1b1.StatefulSetList{}
	if err := r.getAdvancedSTS(ctx, clusterName, listASTS); err != nil {
		return "", fmt.Errorf("get advanced stateful sets: %w", err)
	}
	return InitializeTopologyConf(listASTS), nil
}

func (r *WorkerTopologyReconciler) createDefaultTopologyConfigMap(
	ctx context.Context, req ctrl.Request, slurmCluster *slurmv1.SlurmCluster, logger logr.Logger) error {
	logger.Info("Node topology labels ConfigMap not found, creating with default topology")

	config, err := r.makeDefaultTopologyConfig(ctx, slurmCluster.Name)
	if err != nil {
		return err
	}
	if err := r.updateTopologyConfigMap(ctx, req.Namespace, config); err != nil {
		return err
	}
	logger.Info("Successfully created default topology ConfigMap")
	return nil
}

// EnsureTopologyConfigMap ensures that the ConfigMap for topology configuration exists.
func (r *WorkerTopologyReconciler) EnsureTopologyConfigMap(
	ctx context.Context, namespace, clusterName string,
) error {
	config, err := r.makeDefaultTopologyConfig(ctx, clusterName)
	if err != nil {
		return err
	}
	configMap := r.renderTopologyConfigMap(namespace, config)

	if err = r.Client.Create(ctx, configMap); err != nil {
		return client.IgnoreAlreadyExists(err)
	}

	jailedConfig := r.renderTopologyJailedConfig(namespace)

	if err := r.Client.Create(ctx, jailedConfig); err != nil {
		return client.IgnoreAlreadyExists(err)
	}

	return nil
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

func (r *WorkerTopologyReconciler) updateTopologyConfigMap(ctx context.Context, namespace string, config string) error {
	configMap := r.renderTopologyConfigMap(namespace, config)
	err := r.Client.Patch(ctx, configMap, client.Apply,
		client.ForceOwnership, client.FieldOwner(WorkerTopologyReconcilerName))
	if err != nil {
		return err
	}

	jailedConfig := r.renderTopologyJailedConfig(namespace)

	err = r.Client.Patch(ctx, jailedConfig, client.Apply,
		client.ForceOwnership, client.FieldOwner(WorkerTopologyReconcilerName))
	if err != nil {
		return err
	}

	return nil
}

// getNodeTopologyLabelsConfigMap retrieves the ConfigMap used to store node topology information.
func (r *WorkerTopologyReconciler) getNodeTopologyLabelsConfigMap(ctx context.Context) (*corev1.ConfigMap, error) {
	configMap := &corev1.ConfigMap{
		ObjectMeta: ctrl.ObjectMeta{
			Name:      consts.ConfigMapNameTopologyNodeLabels,
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
