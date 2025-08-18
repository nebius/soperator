package soperatorchecks

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/controller/reconciler"
	"nebius.ai/slurm-operator/internal/controllerconfig"
)

var (
	PodEphemeralStorageCheckName = "soperatorchecks.pod-ephemeral-storage-check"
)

// KubeletStatsAPI structures for parsing kubelet /stats/summary endpoint
type KubeletStats struct {
	Node struct {
		NodeName string `json:"nodeName"`
	} `json:"node"`
	Pods []PodStats `json:"pods"`
}

type PodStats struct {
	PodRef struct {
		Name      string `json:"name"`
		Namespace string `json:"namespace"`
		UID       string `json:"uid"`
	} `json:"podRef"`
	EphemeralStorage struct {
		AvailableBytes *uint64 `json:"availableBytes,omitempty"`
		CapacityBytes  *uint64 `json:"capacityBytes,omitempty"`
		UsedBytes      *uint64 `json:"usedBytes,omitempty"`
	} `json:"ephemeral-storage"`
}

type EphemeralStorageInfo struct {
	PodName      string
	PodNamespace string
	NodeName     string
	UsedBytes    uint64
	LimitBytes   uint64
	UsagePercent float64
}

type PodEphemeralStorageCheck struct {
	*reconciler.Reconciler
	reconcileTimeout time.Duration
	clientset        kubernetes.Interface
	restConfig       *rest.Config
	usageThreshold   float64
}

func NewPodEphemeralStorageCheck(
	client client.Client,
	scheme *runtime.Scheme,
	recorder record.EventRecorder,
	reconcileTimeout time.Duration,
	restConfig *rest.Config,
	usageThreshold float64,
) (*PodEphemeralStorageCheck, error) {
	r := reconciler.NewReconciler(client, scheme, recorder)

	clientset, err := kubernetes.NewForConfig(restConfig)
	if err != nil {
		return nil, fmt.Errorf("creating kubernetes clientset: %w", err)
	}

	return &PodEphemeralStorageCheck{
		Reconciler:       r,
		reconcileTimeout: reconcileTimeout,
		clientset:        clientset,
		restConfig:       restConfig,
		usageThreshold:   usageThreshold,
	}, nil
}

func (r *PodEphemeralStorageCheck) SetupWithManager(mgr ctrl.Manager, maxConcurrency int, cacheSyncTimeout time.Duration) error {
	return ctrl.NewControllerManagedBy(mgr).Named(PodEphemeralStorageCheckName).
		For(&corev1.Pod{}).
		Watches(&kruisev1b1.StatefulSet{},
			handler.EnqueueRequestsFromMapFunc(r.mapKruiseStatefulSetToPods),
		).
		WithEventFilter(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				pod, ok := e.Object.(*corev1.Pod)
				if !ok {
					return false
				}
				return r.isPodRelevant(pod)
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				pod, ok := e.Object.(*corev1.Pod)
				if !ok {
					return false
				}
				return r.isPodRelevant(pod)
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				pod, ok := e.ObjectNew.(*corev1.Pod)
				if !ok {
					return false
				}
				return r.isPodRelevant(pod)
			},
		}).
		WithOptions(controllerconfig.ControllerOptionsWithRateLimit(maxConcurrency, cacheSyncTimeout, 15*time.Second, 1*time.Minute)).
		Complete(r)
}

func (r *PodEphemeralStorageCheck) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName(PodEphemeralStorageCheckName).WithValues(
		"pod", req.Name, "namespace", req.Namespace)

	pod := &corev1.Pod{}
	err := r.Get(ctx, req.NamespacedName, pod)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("getting pod: %w", err)
	}

	if err := r.ReconcilePodEphemeralStorageCheckForPod(ctx, pod); err != nil {
		return ctrl.Result{}, fmt.Errorf("reconciling Pod Ephemeral Storage Check: %w", err)
	}

	logger.Info("Pod Ephemeral Storage Check completed successfully")
	return ctrl.Result{RequeueAfter: r.reconcileTimeout}, nil
}

func (r *PodEphemeralStorageCheck) isPodRelevant(pod *corev1.Pod) bool {
	componentType := pod.Labels[consts.LabelComponentKey]
	managedBy := pod.Labels[consts.LabelManagedByKey]

	if componentType != consts.ComponentTypeWorker.String() ||
		managedBy != consts.LabelManagedByValue {
		return false
	}

	ownerRefs := pod.GetOwnerReferences()
	if len(ownerRefs) == 0 {
		return false
	}

	for _, ownerRef := range ownerRefs {
		if ownerRef.Kind == "StatefulSet" &&
			ownerRef.APIVersion == "apps.kruise.io/v1beta1" {
			return true
		}
	}

	return false
}

// hasOwnerWithSoperator checks if the object has an owner that belongs to soperator
func (r *PodEphemeralStorageCheck) hasOwnerWithSoperator(obj client.Object) bool {
	ownerRefs := obj.GetOwnerReferences()
	for _, ownerRef := range ownerRefs {
		if ownerRef.Kind == "SlurmCluster" &&
			ownerRef.APIVersion == "slurm.nebius.ai/v1" {
			return true
		}
	}
	return false
}

// mapKruiseStatefulSetToPods maps kruise StatefulSet changes to pod reconcile requests
func (r *PodEphemeralStorageCheck) mapKruiseStatefulSetToPods(ctx context.Context, obj client.Object) []reconcile.Request {
	sts, ok := obj.(*kruisev1b1.StatefulSet)
	if !ok {
		return nil
	}
	if !r.hasOwnerWithSoperator(sts) {
		return nil
	}

	return r.getPodsForStatefulSet(ctx, sts.Namespace, sts.Name)
}

// getPodsForStatefulSet returns reconcile requests for pods owned by the StatefulSet
func (r *PodEphemeralStorageCheck) getPodsForStatefulSet(ctx context.Context, namespace, statefulSetName string) []reconcile.Request {
	var podList corev1.PodList
	err := r.List(ctx, &podList, client.InNamespace(namespace), client.MatchingLabels{
		consts.LabelComponentKey: consts.ComponentTypeWorker.String(),
	})
	if err != nil {
		return nil
	}

	var requests []reconcile.Request
	for _, pod := range podList.Items {
		for _, ownerRef := range pod.GetOwnerReferences() {
			if ownerRef.Kind == "StatefulSet" && ownerRef.Name == statefulSetName {
				requests = append(requests, reconcile.Request{
					NamespacedName: types.NamespacedName{
						Name:      pod.Name,
						Namespace: pod.Namespace,
					},
				})
				break
			}
		}
	}

	return requests
}

func (r *PodEphemeralStorageCheck) ReconcilePodEphemeralStorageCheckForPod(ctx context.Context, pod *corev1.Pod) error {
	logger := log.FromContext(ctx).WithName(PodEphemeralStorageCheckName).WithValues(
		"pod", pod.Name, "namespace", pod.Namespace)

	if pod.Status.Phase != corev1.PodRunning || pod.Spec.NodeName == "" {
		logger.V(1).Info("Pod is not running or not assigned to a node, skipping")
		return nil
	}

	storageInfos, err := r.getEphemeralStorageStatsFromNode(ctx, pod.Spec.NodeName, []corev1.Pod{*pod})
	if err != nil {
		logger.Error(err, "Failed to get ephemeral storage stats", "node", pod.Spec.NodeName)
		return err
	}

	for _, info := range storageInfos {
		logger.V(1).Info("Ephemeral storage usage",
			"pod", info.PodName,
			"namespace", info.PodNamespace,
			"node", info.NodeName,
			"usedBytes", info.UsedBytes,
			"limitBytes", info.LimitBytes,
			"usagePercent", fmt.Sprintf("%.2f%%", info.UsagePercent),
		)

		if info.UsagePercent > r.usageThreshold {
			logger.Info("High ephemeral storage usage detected",
				"pod", info.PodName,
				"usagePercent", fmt.Sprintf("%.2f%%", info.UsagePercent),
				"threshold", fmt.Sprintf("%.2f%%", r.usageThreshold),
			)
		}
	}

	return nil
}

func (r *PodEphemeralStorageCheck) findWorkerPods(ctx context.Context, namespace string) ([]corev1.Pod, error) {
	var podList corev1.PodList
	err := r.List(ctx, &podList, client.InNamespace(namespace), client.MatchingLabels{
		consts.LabelComponentKey: consts.ComponentTypeWorker.String(),
	})
	if err != nil {
		return nil, fmt.Errorf("listing worker pods: %w", err)
	}

	var runningPods []corev1.Pod
	for _, pod := range podList.Items {
		if pod.Status.Phase == corev1.PodRunning && pod.Spec.NodeName != "" {
			runningPods = append(runningPods, pod)
		}
	}

	return runningPods, nil
}

func (r *PodEphemeralStorageCheck) getUniqueNodeNames(pods []corev1.Pod) []string {
	nodeSet := make(map[string]bool)
	for _, pod := range pods {
		if pod.Spec.NodeName != "" {
			nodeSet[pod.Spec.NodeName] = true
		}
	}

	var nodeNames []string
	for nodeName := range nodeSet {
		nodeNames = append(nodeNames, nodeName)
	}
	return nodeNames
}

func (r *PodEphemeralStorageCheck) getEphemeralStorageStatsFromNode(ctx context.Context, nodeName string, workerPods []corev1.Pod) ([]EphemeralStorageInfo, error) {
	result := r.clientset.CoreV1().RESTClient().Get().
		Resource("nodes").
		Name(nodeName).
		SubResource("proxy").
		Suffix("stats/summary").
		Do(ctx)

	rawData, err := result.Raw()
	if err != nil {
		return nil, fmt.Errorf("getting kubelet stats from node %s: %w", nodeName, err)
	}

	var stats KubeletStats
	if err := json.Unmarshal(rawData, &stats); err != nil {
		return nil, fmt.Errorf("decoding kubelet stats: %w", err)
	}

	workerPodMap := make(map[string]corev1.Pod)
	for _, pod := range workerPods {
		if pod.Spec.NodeName == nodeName {
			workerPodMap[string(pod.UID)] = pod
		}
	}

	var storageInfos []EphemeralStorageInfo
	for _, podStat := range stats.Pods {
		workerPod, isWorkerPod := workerPodMap[podStat.PodRef.UID]
		if !isWorkerPod {
			continue
		}

		limitBytes := r.getEphemeralStorageLimitForPod(workerPod)
		if limitBytes == 0 {
			continue // Skip pods without ephemeral storage limits
		}

		usedBytes := uint64(0)
		if podStat.EphemeralStorage.UsedBytes != nil {
			usedBytes = *podStat.EphemeralStorage.UsedBytes
		}

		usagePercent := float64(usedBytes) / float64(limitBytes) * 100.0

		storageInfo := EphemeralStorageInfo{
			PodName:      podStat.PodRef.Name,
			PodNamespace: podStat.PodRef.Namespace,
			NodeName:     nodeName,
			UsedBytes:    usedBytes,
			LimitBytes:   limitBytes,
			UsagePercent: usagePercent,
		}

		storageInfos = append(storageInfos, storageInfo)
	}

	return storageInfos, nil
}

func (r *PodEphemeralStorageCheck) getEphemeralStorageLimitForPod(pod corev1.Pod) uint64 {
	var totalLimit int64 = 0

	for _, container := range pod.Spec.Containers {
		if limit, ok := container.Resources.Limits[corev1.ResourceEphemeralStorage]; ok {
			totalLimit += limit.Value()
		}
	}

	for _, container := range pod.Spec.InitContainers {
		if limit, ok := container.Resources.Limits[corev1.ResourceEphemeralStorage]; ok {
			totalLimit += limit.Value()
		}
	}

	return uint64(totalLimit)
}
