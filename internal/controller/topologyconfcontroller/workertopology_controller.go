package topologyconfcontroller

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
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

	existing, err := r.getNodeTopologyLabelsConfigMap(ctx, req, logger)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("get node topology labels ConfigMap: %w", err)
	}
	logger.Info("Retrieved node topology labels ConfigMap", "configMap", existing.Name, "namespace", existing.Namespace)

	desired, err := r.buildNodeSetTopologyConfig(ctx, req.Namespace, slurmCluster)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("build NodeSet topology config: %w", err)
	}
	logger.Info("Built topology config", "topologyConfig", desired)

	existingTopologyConfig := existing.Data[consts.ConfigMapKeyTopologyConfig]
	if r.calculateConfigHash(desired) == r.calculateConfigHash(existingTopologyConfig) {
		logger.Info("Topology config unchanged, skipping update")
		if err := r.ensureJailedConfig(ctx, req.Namespace, slurmCluster.Name); err != nil {
			return ctrl.Result{}, fmt.Errorf("ensure JailedConfig: %w", err)
		}
		return DefaultRequeueResult, nil
	}

	if err := r.updateTopologyConfigMap(ctx, req.Namespace, slurmCluster.Name, desired); err != nil {
		logger.Error(err, "Update ConfigMap with topology config")
		return ctrl.Result{}, fmt.Errorf("update ConfigMap with topology config: %w", err)
	}

	logger.Info("Reconciliation completed successfully")
	return DefaultRequeueResult, nil
}

func isClusterReconciliationNeeded(slurmCluster *slurmv1.SlurmCluster) bool {
	return slurmCluster.Spec.SlurmConfig.TopologyPlugin == consts.SlurmTopologyTree ||
		slurmCluster.Spec.SlurmConfig.TopologyPlugin == consts.SlurmTopologyBlock
}

func topologyConfigMapName(clusterName string) string {
	return clusterName + "-" + consts.ConfigMapNameTopologyConfig
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
			UpdateActions: []v1alpha1.UpdateAction{v1alpha1.UpdateActionReconfigure},
		},
	}
}

// BuildNodeSetTopologyConf builds a topology config for the NodeSet mode.
// All workers are placed under a single "unknown" switch with "root" as the parent.
func BuildNodeSetTopologyConf(nodeSetsList []slurmv1alpha1.NodeSet) string {
	var nodes []string
	for _, ns := range nodeSetsList {
		if ns.Spec.Replicas <= 0 {
			continue
		}
		nodes = append(nodes, formatNodeRange(ns.Name, int(ns.Spec.Replicas)))
	}

	if len(nodes) == 0 {
		return "SwitchName=root Switches=unknown\nSwitchName=unknown"
	}

	return "SwitchName=root Switches=unknown\nSwitchName=unknown Nodes=" + strings.Join(nodes, ",")
}

// formatNodeRange formats a node name with Slurm range notation.
// For 1 replica: "worker-0", for multiple: "worker-[0-N]".
func formatNodeRange(name string, replicas int) string {
	if replicas == 1 {
		return name + "-0"
	}
	return name + "-[0-" + strconv.Itoa(replicas-1) + "]"
}

// buildNodeSetTopologyConfig builds the topology config from NodeSets or worker.Size.
func (r *WorkerTopologyReconciler) buildNodeSetTopologyConfig(
	ctx context.Context, namespace string, slurmCluster *slurmv1.SlurmCluster,
) (string, error) {
	nodeSetsList, err := resourcegetter.ListNodeSetsByClusterRef(
		ctx, r.Client, types.NamespacedName{Namespace: namespace, Name: slurmCluster.Name},
	)
	if err != nil {
		return "", fmt.Errorf("list NodeSets: %w", err)
	}

	return BuildNodeSetTopologyConf(nodeSetsList), nil
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
			cm := &corev1.ConfigMap{
				ObjectMeta: metav1.ObjectMeta{
					Name:      cmName,
					Namespace: namespace,
				},
				Data: map[string]string{
					consts.ConfigMapKeyTopologyConfig: config,
				},
			}
			if err := r.Client.Create(ctx, cm); err != nil {
				return fmt.Errorf("create ConfigMap %s: %w", cmName, err)
			}
		} else {
			return fmt.Errorf("get ConfigMap %s: %w", cmName, err)
		}
	} else {
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

	// Index pod statuses to get client.MatchingFields working.
	if err := mgr.GetFieldIndexer().IndexField(
		context.Background(),
		&corev1.Pod{},
		consts.FieldStatusPhase,
		func(obj client.Object) []string {
			return []string{string(obj.(*corev1.Pod).Status.Phase)}
		},
	); err != nil {
		return fmt.Errorf("failed to setup %s field indexer: %w", consts.FieldStatusPhase, err)
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
			handler.EnqueueRequestsFromMapFunc(r.findSlurmClusterForNodeSet),
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
