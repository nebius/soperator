/*
Copyright 2025 Nebius B.V.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package sconfigcontroller

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v0041 "github.com/SlinkyProject/slurm-client/api/v0041"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/controllerconfig"
	"nebius.ai/slurm-operator/internal/slurmapi"
)

const (
	configMapField = ".spec.configMap"

	reconfigureWaitTimeout = 5 * time.Minute
)

// JailedConfigReconciler reconciles a JailedConfig object
type JailedConfigReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	slurmAPIClient slurmapi.Client
	// TODO use this clock in file store as well
	clock Clock
	fs    FS
}

// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=jailedconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=jailedconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=jailedconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=configmaps,verbs=get;list;watch

// Clock is used to fake timing for testing
type Clock interface {
	Now() time.Time
	Sleep(duration time.Duration)
}

type realClock struct{}

var _ Clock = realClock{}

func (_ realClock) Now() time.Time               { return time.Now() }
func (_ realClock) Sleep(duration time.Duration) { time.Sleep(duration) }

// mostly copy-pasted from k8s ConfigMap volumes
// See https://github.com/kubernetes/kubernetes/blob/b266ac2c3e42c2c4843f81e20213d2b2f43e450a/pkg/volume/configmap/configmap.go/

type JailedFile struct {
	Data []byte
	Mode int32
}

func makePayload(mappings []corev1.KeyToPath, configMap *corev1.ConfigMap, defaultMode *int32) (map[string]JailedFile, error) {
	if defaultMode == nil {
		return nil, fmt.Errorf("no defaultMode used, not even the default value for it")
	}

	payload := make(map[string]JailedFile, len(configMap.Data)+len(configMap.BinaryData))
	var jailedFile JailedFile

	if len(mappings) == 0 {
		for name, data := range configMap.Data {
			jailedFile.Data = []byte(data)
			jailedFile.Mode = *defaultMode
			payload[name] = jailedFile
		}
		for name, data := range configMap.BinaryData {
			jailedFile.Data = data
			jailedFile.Mode = *defaultMode
			payload[name] = jailedFile
		}
	} else {
		for _, ktp := range mappings {
			if stringData, ok := configMap.Data[ktp.Key]; ok {
				jailedFile.Data = []byte(stringData)
			} else if binaryData, ok := configMap.BinaryData[ktp.Key]; ok {
				jailedFile.Data = binaryData
			} else {
				return nil, fmt.Errorf("configmap references non-existent config key: %s", ktp.Key)
			}

			if ktp.Mode != nil {
				jailedFile.Mode = *ktp.Mode
			} else {
				jailedFile.Mode = *defaultMode
			}
			payload[ktp.Path] = jailedFile
		}
	}

	return payload, nil
}

func validatePayloadPath(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("invalid path %q: must be absolute", path)
	}
	parts := filepath.SplitList(path)
	for _, part := range parts {
		if part == "." || part == ".." {
			return fmt.Errorf("invalid path %q: must not contain path traversal", path)
		}
	}
	return nil
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the JailedConfig object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *JailedConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	// TODo add more fields
	logger := logf.FromContext(ctx)

	logger.V(1).Info("Reconciling JailedConfig", "req", req)

	jailedConfig := &slurmv1alpha1.JailedConfig{}
	err = r.Client.Get(ctx, req.NamespacedName, jailedConfig)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// JailedConfig not found, so it must have been deleted
			// There's basically just 3 ways to react to deletion:
			// * Do nothing: materialized files will remain in jail FS; simplest option
			// * Unlink file from FS
			// * Truncate/replace file with empty one
			// Replacing files with tombstones is not universal: tombstone can't be part of resource,
			// and hardcoding tombstone here is not flexible for arbitrary file format
			// Unlinking and truncating can lead to issues if between deleting resource and deleting file user will change it, but that's expected

			logger.V(1).Info("JailedConfig resource not found. Ignoring since object must have been be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		return ctrl.Result{}, fmt.Errorf("getting JailedConfig: %w", err)
	}

	err = r.initializeConditions(ctx, jailedConfig)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("initializing conditions: %w", err)
	}

	err = r.setConditions(
		ctx,
		jailedConfig,
		metav1.Condition{
			Type:    string(slurmv1alpha1.FilesWritten),
			Status:  metav1.ConditionFalse,
			Reason:  slurmv1alpha1.ReasonRefresh,
			Message: "Refreshing files in jail FS",
		},
		metav1.Condition{
			Type:    string(slurmv1alpha1.UpdateActionsCompleted),
			Status:  metav1.ConditionFalse,
			Reason:  slurmv1alpha1.ReasonRefresh,
			Message: "Refreshing files in jail FS",
		},
	)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("setting conditions: %w", err)
	}

	configMapName := jailedConfig.Spec.ConfigMap.Name
	configMapNamespace := jailedConfig.Spec.ConfigMap.Namespace
	if configMapNamespace != "" && configMapNamespace != jailedConfig.Namespace {
		// JailedConfig tries to reference ConfigMap from another namespace
		// This is incorrect spec, it does not make sense to requeue this error, only spec change would help
		return ctrl.Result{}, reconcile.TerminalError(
			fmt.Errorf(
				"namespace mismatch: JailedConfig %s vs ConfigMap %s",
				jailedConfig.Namespace,
				configMapNamespace,
			),
		)
	}

	configMap := &corev1.ConfigMap{}
	err = r.Client.Get(ctx, types.NamespacedName{
		Name:      configMapName,
		Namespace: jailedConfig.Namespace,
	}, configMap)
	if err != nil {
		// Error reading the object - requeue the request.
		return ctrl.Result{}, fmt.Errorf("getting ConfigMap: %w", err)
	}

	if len(jailedConfig.Spec.Items) == 0 {
		// TODO this return is happening, but reconciliation keeps looping
		return ctrl.Result{}, nil
	}

	filesBatch := NewReplacedFilesBatch(r.fs)
	defer func() {
		err = errors.Join(err, filesBatch.Cleanup())
	}()

	jailPayload, err := makePayload(jailedConfig.Spec.Items, configMap, jailedConfig.Spec.DefaultMode)
	if err != nil {
		// Error preparing payload - requeue the request.
		return ctrl.Result{}, fmt.Errorf("making JailedConfig payload: %w", err)
	}

	for path, payload := range jailPayload {
		if err := validatePayloadPath(path); err != nil {
			return ctrl.Result{}, fmt.Errorf("invalid path %q in ConfigMap annotations: %w", path, err)
		}

		err = filesBatch.Replace(path, payload.Data, uint32(payload.Mode))
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("replacing file %q in FS: %w", path, err)
		}
	}

	err = r.setConditions(
		ctx,
		jailedConfig,
		metav1.Condition{
			Type:    string(slurmv1alpha1.FilesWritten),
			Status:  metav1.ConditionTrue,
			Reason:  slurmv1alpha1.ReasonSuccess,
			Message: "Files were written to jail FS",
		},
	)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("setting conditions: %w", err)
	}

	err = filesBatch.Finish()
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("finishing replacing files in FS: %w", err)
	}

	for _, action := range jailedConfig.Spec.UpdateActions {
		switch action {
		case slurmv1alpha1.Reconfigure:
			err = r.reconfigureCluster(ctx)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("reconfiguring Slurm cluster: %w", err)
			}
		default:
			return ctrl.Result{}, fmt.Errorf("unexpected update action %s: %w", action, err)
		}
	}

	err = r.setConditions(
		ctx,
		jailedConfig,
		metav1.Condition{
			Type:    string(slurmv1alpha1.UpdateActionsCompleted),
			Status:  metav1.ConditionTrue,
			Reason:  slurmv1alpha1.ReasonSuccess,
			Message: "Update actions were called successfully",
		},
	)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("setting conditions: %w", err)
	}

	return ctrl.Result{}, nil
}

func NewJailedConfigReconciler(
	client client.Client,
	scheme *runtime.Scheme,
	slurmAPIClient slurmapi.Client,
	fs FS,
) *JailedConfigReconciler {
	return &JailedConfigReconciler{
		Client:         client,
		Scheme:         scheme,
		slurmAPIClient: slurmAPIClient,
		fs:             fs,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *JailedConfigReconciler) SetupWithManager(mgr ctrl.Manager, maxConcurrency int, cacheSyncTimeout time.Duration) error {
	if r.clock == nil {
		r.clock = realClock{}
	}

	// TODO fix this to work with object reference
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &slurmv1alpha1.JailedConfig{}, configMapField, func(rawObj client.Object) []string {
		jailedConfig := rawObj.(*slurmv1alpha1.JailedConfig)
		if jailedConfig.Spec.ConfigMap == nil {
			return nil
		}
		if jailedConfig.Spec.ConfigMap.Name == "" {
			return nil
		}
		return []string{jailedConfig.Spec.ConfigMap.Name}
	}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&slurmv1alpha1.JailedConfig{}, builder.WithPredicates(predicate.GenerationChangedPredicate{})).
		Watches(
			&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(r.findObjectsForConfigMap),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Named("jailedconfig").
		WithOptions(controllerconfig.ControllerOptions(maxConcurrency, cacheSyncTimeout)).
		Complete(r)
}

func (r *JailedConfigReconciler) findObjectsForConfigMap(ctx context.Context, configMapObject client.Object) []reconcile.Request {
	// TODO filter incoming configmap by namespace from CLI

	jailedConfigs := &slurmv1alpha1.JailedConfigList{}
	listOps := &client.ListOptions{
		// TODO fix this to work with object reference
		FieldSelector: fields.OneTermEqualSelector(configMapField, configMapObject.GetName()),
		Namespace:     configMapObject.GetNamespace(),
	}
	err := r.List(ctx, jailedConfigs, listOps)
	if err != nil {
		return []reconcile.Request{}
	}

	requests := make([]reconcile.Request, len(jailedConfigs.Items))
	for i, item := range jailedConfigs.Items {
		requests[i] = reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name:      item.GetName(),
				Namespace: item.GetNamespace(),
			},
		}
	}
	return requests
}

type hasStatusCode interface {
	StatusCode() int
}

func checkStatus[R hasStatusCode](response R) error {
	if response.StatusCode() != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", response.StatusCode())
	}

	return nil
}

func checkApiErrors(responseErrors *v0041.V0041OpenapiErrors) error {
	if len(*responseErrors) > 0 {
		errs := make([]error, 0)
		for _, err := range *responseErrors {
			errs = append(errs, fmt.Errorf("API error %d %s, source %s", err.ErrorNumber, *err.Error, *err.Source))
		}
		return errors.Join(errs...)
	}

	return nil
}

func (r *JailedConfigReconciler) getNodes(ctx context.Context) (*v0041.V0041OpenapiNodesResp, error) {
	nodesBefore, err := r.slurmAPIClient.SlurmV0041GetNodesWithResponse(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("listing workers via Slurm API: %w", err)
	}
	if err = checkStatus(nodesBefore); err != nil {
		return nil, fmt.Errorf("listing workers via Slurm API: %w", err)
	}
	if err = checkApiErrors(nodesBefore.JSON200.Errors); err != nil {
		return nil, fmt.Errorf("listing workers via Slurm API: %w", err)
	}

	return nodesBefore.JSON200, nil
}

func (r *JailedConfigReconciler) getNodesStartTime(ctx context.Context) (map[string]int64, error) {
	nodes, err := r.getNodes(ctx)
	if err != nil {
		return nil, err
	}

	nodeToStart := make(map[string]int64)
	for _, node := range nodes.Nodes {
		name := *node.Name
		if _, ok := nodeToStart[name]; ok {
			return nil, fmt.Errorf("duplicated worker name in Slurm API: %s", name)
		}

		if *node.SlurmdStartTime.Infinite {
			return nil, fmt.Errorf("unexpected infinite start time for worker in Slurm API: %s", name)
		}
		if !*node.SlurmdStartTime.Set {
			return nil, fmt.Errorf("unexpected unset start time for worker in Slurm API: %s", name)
		}
		slurmdStartTime := *node.SlurmdStartTime.Number

		nodeToStart[name] = slurmdStartTime
	}

	return nodeToStart, nil
}

// Reconfigure REST endpoint will trigger reconfigure on slurm controller
// REST API would queue this request and wait for response
// Controller (in daemon mode) would fork, and parent will wait until child is ready, and then respond to reconfigure requests
// Also child would queue reconfigure messages to all workers. But neither parent nor child would wait for workers to finish
// See https://github.com/SchedMD/slurm/blob/e39bf73e8194d237582d3f5561d2688d4aee45d3/src/slurmrestd/plugins/openapi/slurmctld/control.c#L46
// See https://github.com/SchedMD/slurm/blob/e39bf73e8194d237582d3f5561d2688d4aee45d3/src/api/reconfigure.c#L64
// See https://github.com/SchedMD/slurm/blob/e39bf73e8194d237582d3f5561d2688d4aee45d3/src/slurmctld/proc_req.c#L6621-L6623
// See https://github.com/SchedMD/slurm/blob/e39bf73e8194d237582d3f5561d2688d4aee45d3/src/slurmctld/proc_req.c#L3128
// See https://github.com/SchedMD/slurm/blob/e39bf73e8194d237582d3f5561d2688d4aee45d3/src/slurmctld/controller.c#L333-L335
// See https://github.com/SchedMD/slurm/blob/e39bf73e8194d237582d3f5561d2688d4aee45d3/src/slurmctld/controller.c#L1369
// See https://github.com/SchedMD/slurm/blob/e39bf73e8194d237582d3f5561d2688d4aee45d3/src/slurmctld/controller.c#L993-L998
// So, if this action would only wait for reconfigure REST response, and then would try to do one more reconciliation
// it is possible to change config files right when worker was restarting, and worker can observe inconsistent FS state
// There is no simple way to check something like worker generation, so this will check that slurmd start time is changed
// Pattern like this is used in slurm to wait for node reboot
// See https://github.com/SchedMD/slurm/blob/dff6513dc96ae422dda876b22e64ee9149c418ec/src/slurmctld/node_mgr.c#L4539-L4551
func (r *JailedConfigReconciler) reconfigureCluster(ctx context.Context) error {
	logger := logf.FromContext(ctx)

	logger.V(1).Info("Storing workers start times before reconfigure")

	// TODO track not just that time has changed, but also that new time is higher than time of request (that would require high degree of clock synchronicity between this controller and worker pods)
	nodeToStartBefore, err := r.getNodesStartTime(ctx)
	if err != nil {
		return err
	}

	logger.V(1).Info("Requesting Slurm API to reconfigure workers")
	reconfigureResponse, err := r.slurmAPIClient.SlurmV0041GetReconfigureWithResponse(ctx)
	if err != nil {
		return fmt.Errorf("requesting Slurm API to reconfigure workers: %w", err)
	}
	if err = checkStatus(reconfigureResponse); err != nil {
		return fmt.Errorf("reconfigure via Slurm API: %w", err)
	}
	if err = checkApiErrors(reconfigureResponse.JSON200.Errors); err != nil {
		return fmt.Errorf("reconfigure via Slurm API: %w", err)
	}

	reconfigureWaitDeadline := r.clock.Now().Add(reconfigureWaitTimeout)
	for {
		if r.clock.Now().After(reconfigureWaitDeadline) {
			return fmt.Errorf("nodes did not restart before deadline exceeded: %s", reconfigureWaitDeadline)
		}

		logger.V(1).Info("Checking workers start times after reconfigure")

		nodeToStartAfter, err := r.getNodesStartTime(ctx)
		if err != nil {
			return err
		}

		for name, slurmdStartTimeAfter := range nodeToStartAfter {
			slurmdStartTimeBefore, ok := nodeToStartBefore[name]
			if !ok {
				// Either node already changed its start time, or was not present before reconfigure
				// Assuming new node already has new config
				continue
			}

			if slurmdStartTimeAfter > slurmdStartTimeBefore {
				// Start time increased, assuming worker has restarted and picker up new config
				delete(nodeToStartBefore, name)
			}
		}

		if len(nodeToStartBefore) == 0 {
			break
		}

		r.clock.Sleep(1 * time.Second)
	}

	return nil
}

type statusPatcher func(status *slurmv1alpha1.JailedConfigStatus)

func (r *JailedConfigReconciler) patchStatus(ctx context.Context, jailedConfig *slurmv1alpha1.JailedConfig, patcher statusPatcher) error {
	patch := client.MergeFrom(jailedConfig.DeepCopy())
	patcher(&jailedConfig.Status)

	if err := r.Status().Patch(ctx, jailedConfig, patch); err != nil {
		return fmt.Errorf("patching JailedConfig status: %w", err)
	}

	return nil
}

func (r *JailedConfigReconciler) initializeCondition(status *slurmv1alpha1.JailedConfigStatus, cond slurmv1alpha1.JailedConfigConditionType) {
	if meta.FindStatusCondition(status.Conditions, string(metav1.ConditionUnknown)) != nil {
		// Do nothing
	}

	_ = meta.SetStatusCondition(&status.Conditions, metav1.Condition{
		Type:    string(cond),
		Status:  metav1.ConditionUnknown,
		Reason:  slurmv1alpha1.ReasonInit,
		Message: "Conditions was just initialized",
	})
}

func (r *JailedConfigReconciler) initializeConditions(ctx context.Context, jailedConfig *slurmv1alpha1.JailedConfig) error {
	return r.patchStatus(ctx, jailedConfig, func(status *slurmv1alpha1.JailedConfigStatus) {
		r.initializeCondition(status, slurmv1alpha1.FilesWritten)
		r.initializeCondition(status, slurmv1alpha1.UpdateActionsCompleted)
	})
}

func (r *JailedConfigReconciler) setConditions(ctx context.Context, jailedConfig *slurmv1alpha1.JailedConfig, conditions ...metav1.Condition) error {
	return r.patchStatus(ctx, jailedConfig, func(status *slurmv1alpha1.JailedConfigStatus) {
		for _, cond := range conditions {
			_ = meta.SetStatusCondition(&status.Conditions, cond)
		}
	})
}
