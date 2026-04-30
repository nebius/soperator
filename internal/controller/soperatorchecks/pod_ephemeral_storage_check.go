package soperatorchecks

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	api "github.com/SlinkyProject/slurm-client/api/v0041"
	kruisev1b1 "github.com/openkruise/kruise-api/apps/v1beta1"
	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/controller/reconciler"
	"nebius.ai/slurm-operator/internal/controllerconfig"
	"nebius.ai/slurm-operator/internal/jwt"
	"nebius.ai/slurm-operator/internal/naming"
	"nebius.ai/slurm-operator/internal/slurmapi"
)

// +kubebuilder:rbac:groups=core,resources=pods,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=nodes,verbs=get;list;watch
// +kubebuilder:rbac:groups=core,resources=nodes/proxy,verbs=get;watch;list
// +kubebuilder:rbac:groups=core,resources=events,verbs=create;patch;list;watch;get;update
// +kubebuilder:rbac:groups=apps.kruise.io,resources=statefulsets,verbs=get;list;watch;
// +kubebuilder:rbac:groups=apps.kruise.io,resources=statefulsets,verbs=get;list;watch;

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
	resumeThreshold  float64
	slurmAPIClients  *slurmapi.ClientSet
}

func NewPodEphemeralStorageCheck(
	client client.Client,
	scheme *runtime.Scheme,
	recorder record.EventRecorder,
	restConfig *rest.Config,
	reconcileTimeout time.Duration,
	usageThreshold float64,
	resumeThreshold float64,
	slurmAPIClients *slurmapi.ClientSet,
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
		resumeThreshold:  resumeThreshold,
		slurmAPIClients:  slurmAPIClients,
	}, nil
}

func (r *PodEphemeralStorageCheck) SetupWithManager(mgr ctrl.Manager, maxConcurrency int, cacheSyncTimeout time.Duration) error {
	ctx := context.Background()
	if err := mgr.GetFieldIndexer().IndexField(ctx, &corev1.Event{}, "involvedObject.name", func(rawObj client.Object) []string {
		event := rawObj.(*corev1.Event)
		return []string{event.InvolvedObject.Name}
	}); err != nil {
		return fmt.Errorf("setting up involvedObject.name indexer: %w", err)
	}

	if err := mgr.GetFieldIndexer().IndexField(ctx, &corev1.Event{}, "involvedObject.uid", func(rawObj client.Object) []string {
		event := rawObj.(*corev1.Event)
		return []string{string(event.InvolvedObject.UID)}
	}); err != nil {
		return fmt.Errorf("setting up involvedObject.uid indexer: %w", err)
	}

	if err := mgr.GetFieldIndexer().IndexField(ctx, &corev1.Event{}, "reason", func(rawObj client.Object) []string {
		event := rawObj.(*corev1.Event)
		return []string{event.Reason}
	}); err != nil {
		return fmt.Errorf("setting up reason indexer: %w", err)
	}

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
				return false
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				return false
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
	componentType := pod.Labels[consts.LabelWorkerKey]
	managedBy := pod.Labels[consts.LabelManagedByKey]

	if componentType != consts.LabelWorkerValue ||
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
		consts.LabelWorkerKey: consts.LabelWorkerValue,
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
		return fmt.Errorf("getting ephemeral storage stats: %w, pod: %s/%s", err, pod.Namespace, pod.Name)
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
			if err := r.handleHighStorageUsage(ctx, pod, info); err != nil {
				return err
			}
		} else if info.UsagePercent < r.resumeThreshold {
			if err := r.handleLowStorageUsage(ctx, pod); err != nil {
				return err
			}
		}

	}

	return nil
}

func (r *PodEphemeralStorageCheck) initSlurmClientAndGetNode(
	ctx context.Context, pod *corev1.Pod,
) (types.NamespacedName, slurmapi.Node, error) {
	slurmClusterName, err := r.getSlurmClusterName(ctx, pod.Namespace)
	if err != nil {
		return types.NamespacedName{}, slurmapi.Node{}, fmt.Errorf("getting SlurmCluster name: %w for pod %s/%s", err, pod.Namespace, pod.Name)
	}
	if slurmClusterName == "" {
		return types.NamespacedName{}, slurmapi.Node{}, fmt.Errorf("not found SlurmCluster for pod %s/%s", pod.Namespace, pod.Name)
	}

	slurmClusterNamespacedName := types.NamespacedName{
		Name:      slurmClusterName,
		Namespace: pod.Namespace,
	}

	if err := r.ensureNodeCache(ctx, slurmClusterNamespacedName, slurmClusterName); err != nil {
		return types.NamespacedName{}, slurmapi.Node{}, fmt.Errorf("initializing Slurm API clients: %w", err)
	}

	nc, found := r.slurmAPIClients.GetNodeCache(slurmClusterNamespacedName)
	if !found {
		return types.NamespacedName{}, slurmapi.Node{}, fmt.Errorf("node cache not found for cluster %v", slurmClusterNamespacedName)
	}

	slurmNode, found := nc.GetNode(pod.Name)
	if !found {
		return types.NamespacedName{}, slurmapi.Node{}, fmt.Errorf("slurm node %s not found in cache for cluster %v", pod.Name, slurmClusterNamespacedName)
	}
	return slurmClusterNamespacedName, slurmNode, nil
}

func (r *PodEphemeralStorageCheck) handleHighStorageUsage(ctx context.Context, pod *corev1.Pod, info EphemeralStorageInfo) error {
	logger := log.FromContext(ctx).WithName(PodEphemeralStorageCheckName)

	logger.Info("High ephemeral storage usage detected",
		"pod", info.PodName,
		"usagePercent", fmt.Sprintf("%.2f%%", info.UsagePercent),
		"threshold", fmt.Sprintf("%.2f%%", r.usageThreshold),
	)

	if err := r.createEphemeralStorageEvent(ctx, pod, info); err != nil {
		return err
	}

	slurmClusterNamespacedName, slurmNode, err := r.initSlurmClientAndGetNode(ctx, pod)
	if err != nil {
		return err
	}

	if err := r.checkSlurmNodeDrainStatus(ctx, slurmNode, pod); err != nil {
		if err.Error() == "node needs draining" {
			if err := r.drainSlurmNode(ctx, slurmClusterNamespacedName, slurmNode.Name, info); err != nil {
				return fmt.Errorf("draining Slurm node: %w for pod %s/%s", err, pod.Namespace, pod.Name)
			}
		} else {
			return err
		}
	}

	return nil
}

func (r *PodEphemeralStorageCheck) createEphemeralStorageEvent(ctx context.Context, pod *corev1.Pod, info EphemeralStorageInfo) error {
	now := metav1.Now()

	event := &corev1.Event{
		ObjectMeta: metav1.ObjectMeta{
			Namespace:    pod.Namespace,
			GenerateName: fmt.Sprintf("%s-ephemeral-storage-", pod.Name),
			Labels: map[string]string{
				consts.LabelWorkerKey:    consts.LabelWorkerValue,
				consts.LabelManagedByKey: consts.LabelManagedByValue,
			},
		},
		InvolvedObject: corev1.ObjectReference{
			Kind:            "Pod",
			Name:            pod.Name,
			Namespace:       pod.Namespace,
			UID:             pod.UID,
			APIVersion:      "v1",
			ResourceVersion: pod.ResourceVersion,
		},
		Reason: consts.HighEphemeralStorageUsage,
		Message: fmt.Sprintf("Pod %s in namespace %s is using %.2f%% of its ephemeral storage limit (%d bytes used, %d bytes limit)",
			info.PodName, info.PodNamespace,
			info.UsagePercent, info.UsedBytes, info.LimitBytes),
		Type:                corev1.EventTypeWarning,
		Count:               1,
		FirstTimestamp:      now,
		LastTimestamp:       now,
		ReportingController: PodEphemeralStorageCheckName,
		ReportingInstance:   PodEphemeralStorageCheckName,
	}

	eventList := &corev1.EventList{}
	err := r.List(ctx, eventList,
		client.InNamespace(pod.Namespace),
		client.MatchingFields{
			"involvedObject.name": pod.Name,
			"involvedObject.uid":  string(pod.UID),
			"reason":              consts.HighEphemeralStorageUsage,
		},
	)
	if err != nil {
		err = r.List(ctx, eventList, client.InNamespace(pod.Namespace))
		if err != nil {
			return r.Client.Create(ctx, event)
		}
	}

	oneHourAgo := metav1.NewTime(now.Add(-time.Hour))
	for i := range eventList.Items {
		existingEvent := &eventList.Items[i]
		if existingEvent.InvolvedObject.Name == pod.Name &&
			existingEvent.InvolvedObject.UID == pod.UID &&
			existingEvent.Reason == consts.HighEphemeralStorageUsage &&
			existingEvent.LastTimestamp.After(oneHourAgo.Time) &&
			existingEvent.Message == event.Message {
			existingEvent.Count++
			existingEvent.LastTimestamp = now
			return r.Client.Update(ctx, existingEvent)
		}
	}
	return r.Client.Create(ctx, event)
}

func (r *PodEphemeralStorageCheck) checkSlurmNodeDrainStatus(ctx context.Context, slurmNodeName slurmapi.Node, pod *corev1.Pod) error {
	logger := log.FromContext(ctx).WithName(PodEphemeralStorageCheckName)

	if slurmNodeName.Name == "" {
		return fmt.Errorf("slurm node not found for pod %s/%s", pod.Namespace, pod.Name)
	}
	logger.Info("slurm node", "nodeStates", slurmNodeName.States)
	if slurmNodeName.IsIdleDrained() {
		return nil
	}

	return fmt.Errorf("node needs draining")
}

func (r *PodEphemeralStorageCheck) findWorkerPods(ctx context.Context, namespace string) ([]corev1.Pod, error) {
	var podList corev1.PodList
	err := r.List(ctx, &podList, client.InNamespace(namespace), client.MatchingLabels{
		consts.LabelWorkerKey: consts.LabelWorkerValue,
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

func (r *PodEphemeralStorageCheck) getSlurmClusterName(ctx context.Context, namespace string) (string, error) {
	SlurmClusterList := &slurmv1.SlurmClusterList{}
	if err := r.List(
		ctx, SlurmClusterList, client.InNamespace(namespace),
	); err != nil {
		return "", fmt.Errorf("listing SlurmCluster in namespace %s: %w", namespace, err)
	}
	slurmClusterName := ""
	if len(SlurmClusterList.Items) > 0 {
		slurmClusterName = SlurmClusterList.Items[0].Name
	}

	return slurmClusterName, nil
}

// ensureNodeCache creates the Slurm API client and starts the node cache background
// goroutine for the given cluster if they do not already exist, then blocks until
// the first successful ListNodes refresh completes.
func (r *PodEphemeralStorageCheck) ensureNodeCache(
	ctx context.Context,
	slurmClusterNamespacedName types.NamespacedName,
	slurmClusterName string,
) error {
	// Always wait for readiness, even when cache already exists — a concurrent
	// reconcile may have just created it and the first refresh is still in flight.
	if nc, found := r.slurmAPIClients.GetNodeCache(slurmClusterNamespacedName); found {
		return nc.WaitReady(ctx)
	}

	if _, found := r.slurmAPIClients.GetClient(slurmClusterNamespacedName); !found {
		jwtToken := jwt.NewToken(r.Client).For(slurmClusterNamespacedName, "root").WithRegistry(jwt.NewTokenRegistry().Build())
		slurmAPIServer := fmt.Sprintf("http://%s.%s:6820", naming.BuildServiceName(consts.ComponentTypeREST, slurmClusterName), slurmClusterNamespacedName.Namespace)
		slurmAPIClient, err := slurmapi.NewClient(slurmAPIServer, jwtToken, slurmapi.DefaultHTTPClient())
		if err != nil {
			return fmt.Errorf("creating slurm api client: %w", err)
		}
		r.slurmAPIClients.AddClient(slurmClusterNamespacedName, slurmAPIClient)
	}

	nc := r.slurmAPIClients.EnsureNodeCache(
		slurmClusterNamespacedName,
		r.reconcileTimeout,
		log.Log.WithName("NodeCache").WithValues("cluster", slurmClusterNamespacedName),
	)
	if nc == nil {
		return fmt.Errorf("no slurm API client for cluster %v", slurmClusterNamespacedName)
	}
	return nc.WaitReady(ctx)
}

func (r *PodEphemeralStorageCheck) handleLowStorageUsage(ctx context.Context, pod *corev1.Pod) error {
	slurmClusterNamespacedName, slurmNode, err := r.initSlurmClientAndGetNode(ctx, pod)
	if err != nil {
		return err
	}

	if !r.isDrainedByEphemeralStorageCheck(slurmNode) {
		return nil
	}

	return r.undrainSlurmNode(ctx, slurmClusterNamespacedName, slurmNode.Name)
}

func (r *PodEphemeralStorageCheck) isDrainedByEphemeralStorageCheck(node slurmapi.Node) bool {
	if !node.IsDrainState() {
		return false
	}
	if node.Reason == nil {
		return false
	}
	return strings.HasPrefix(node.Reason.Reason, consts.SlurmUserReasonHC+" pod_ephemeral_storage")
}

func (c *PodEphemeralStorageCheck) undrainSlurmNode(
	ctx context.Context,
	slurmClusterName types.NamespacedName,
	slurmNodeName string,
) error {
	logger := log.FromContext(ctx).WithName(PodEphemeralStorageCheckName).
		WithValues(
			"slurmNodeName", slurmNodeName,
			"slurmCluster", slurmClusterName,
		)
	logger.Info("undraining slurm node after ephemeral storage usage dropped below threshold")

	slurmAPIClient, found := c.slurmAPIClients.GetClient(slurmClusterName)
	if !found {
		return fmt.Errorf("slurm cluster %v not found", slurmClusterName)
	}

	resp, err := slurmAPIClient.SlurmV0041PostNodeWithResponse(ctx, slurmNodeName,
		api.V0041UpdateNodeMsg{
			State: ptr.To([]api.V0041UpdateNodeMsgState{api.V0041UpdateNodeMsgStateRESUME}),
		},
	)
	if err != nil {
		return fmt.Errorf("post undrain slurm node: %w", err)
	}
	if resp.JSON200.Errors != nil && len(*resp.JSON200.Errors) != 0 {
		return fmt.Errorf("post undrain returned errors: %v", *resp.JSON200.Errors)
	}

	logger.Info("slurm node state is updated to RESUME")
	return nil
}

func (c *PodEphemeralStorageCheck) drainSlurmNode(
	ctx context.Context,
	slurmClusterName types.NamespacedName,
	slurmNodeName string,
	info EphemeralStorageInfo,
) error {
	message := fmt.Sprintf(
		"pod_ephemeral_storage %.2[1]f%% of ephemeral storage is used. Clean up volumes from 'ssh %[2]s /opt/soperator_utils/fs_usage.sh -l', "+
			"delete leftover containers from 'ssh %[2]s enroot list' and 'ssh %[2]s docker ps -a', "+
			"reboot the node using 'scontrol reboot %[2]s', "+
			"or stop-start the InstanceId from 'scontrol show node %[2]s'. And 'scontrol update nodename=%[2]s state=resume' after resolving the issue.",
		info.UsagePercent, slurmNodeName,
	)
	reason := consts.SlurmUserReasonHC + " " + message
	logger := log.FromContext(ctx).WithName("SlurmNodesController.drainSlurmNode").
		WithValues(
			"slurmNodeName", slurmNodeName,
			"drainReason", reason,
			"slurmCluster", slurmClusterName,
		)
	logger.Info("draining slurm node")

	slurmAPIClient, found := c.slurmAPIClients.GetClient(slurmClusterName)
	if !found {
		return fmt.Errorf("slurm cluster %v not found", slurmClusterName)
	}

	resp, err := slurmAPIClient.SlurmV0041PostNodeWithResponse(ctx, slurmNodeName,
		api.V0041UpdateNodeMsg{
			Reason: ptr.To(string(reason)),
			State:  ptr.To([]api.V0041UpdateNodeMsgState{api.V0041UpdateNodeMsgStateDRAIN}),
		},
	)
	if err != nil {
		return fmt.Errorf("post drain slurm node: %w", err)
	}
	if resp.JSON200.Errors != nil && len(*resp.JSON200.Errors) != 0 {
		return fmt.Errorf("post drain returned errors: %v", *resp.JSON200.Errors)
	}

	logger.V(1).Info("slurm node state is updated to DRAIN")
	return nil
}
