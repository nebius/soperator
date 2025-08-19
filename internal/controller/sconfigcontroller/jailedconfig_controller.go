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
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/workqueue"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v0041 "github.com/SlinkyProject/slurm-client/api/v0041"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/logfield"
	"nebius.ai/slurm-operator/internal/slurmapi"
)

const (
	configMapField = ".spec.configMap.name"

	defaultReconfigureWaitTimeout  = 1 * time.Minute
	defaultReconfigurePollInterval = 20 * time.Second
)

// JailedConfigReconciler reconciles a JailedConfig object
type JailedConfigReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	slurmAPIClient          slurmapi.Client
	clock                   Clock
	fs                      Fs
	reconfigurePollInterval time.Duration
	reconfigureWaitTimeout  time.Duration
}

// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=jailedconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=jailedconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=jailedconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups="core",resources=configmaps,verbs=get;list;watch;patch

// Clock is used to fake timing for testing
type Clock interface {
	After(duration time.Duration) <-chan time.Time
}

type realClock struct{}

var _ Clock = realClock{}

func (_ realClock) After(duration time.Duration) <-chan time.Time { return time.After(duration) }

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
				return nil, fmt.Errorf("JailedConfig items references non-existent config key: %s", ktp.Key)
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

// validatePayloadPath should validate path from spec PoV. So, relative paths are a spec issue, but
// path traversals and symlinks are implementation limitations, and permissions are just current state
func validatePayloadPath(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("must be absolute")
	}
	return nil
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.21.0/pkg/reconcile
func (r *JailedConfigReconciler) Reconcile(ctx context.Context, req ctrl.Request) (res ctrl.Result, err error) {
	logger := logf.FromContext(ctx).WithValues(
		logfield.JailedConfigNamespace, req.Namespace,
		logfield.JailedConfigName, req.Name,
	)
	ctx = logf.IntoContext(ctx, logger)

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
		return ctrl.Result{}, fmt.Errorf("getting JailedConfig: %w", err)
	}

	// Check if this JailedConfig has aggregation key
	aggregationKey, hasAggregationKey := jailedConfig.Labels[consts.LabelJailedAggregationKey]
	if !hasAggregationKey {
		// Process individual JailedConfig without aggregation
		return r.reconcileIndividual(ctx, jailedConfig)
	}

	// Process JailedConfig with aggregation
	return r.reconcileWithAggregation(ctx, jailedConfig, aggregationKey)
}

func (r *JailedConfigReconciler) reconcileIndividual(ctx context.Context, jailedConfig *slurmv1alpha1.JailedConfig) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	err := r.shouldInitializeConditions(ctx, jailedConfig)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("initializing conditions: %w", err)
	}

	configMap := &corev1.ConfigMap{}
	needsReconcile, err := r.shouldReconciliation(ctx, jailedConfig, configMap)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("checking if reconciliation should be skipped: %w", err)
	}
	if !needsReconcile {
		logger.V(1).Info("Skipping reconciliation: config unchanged and conditions met")
		return ctrl.Result{}, nil
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

	defaultMode := jailedConfig.Spec.DefaultMode
	if defaultMode == nil {
		defaultMode = ptr.To(slurmv1alpha1.DefaultMode)
	}

	jailPayload, err := makePayload(jailedConfig.Spec.Items, configMap, defaultMode)
	if err != nil {
		return ctrl.Result{}, reconcile.TerminalError(fmt.Errorf("making JailedConfig payload: %w", err))
	}

	for path := range jailPayload {
		if err := validatePayloadPath(path); err != nil {
			return ctrl.Result{}, reconcile.TerminalError(fmt.Errorf("invalid config path %q: %w", path, err))
		}
	}

	logger.V(1).Info("Going to write files", logfield.JailedConfigFilesCount, len(jailPayload))

	filesBatch := NewReplacedFilesBatch(r.fs)
	defer func() {
		err = errors.Join(err, filesBatch.Cleanup())
	}()

	for path, payload := range jailPayload {
		err = filesBatch.Replace(path, payload.Data, os.FileMode(payload.Mode))
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("replacing file %q in FS: %w", path, err)
		}
	}

	logger.V(1).Info("Done writing files")
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

	logger.V(1).Info("Finished syncing caches for written files")

	for _, action := range jailedConfig.Spec.UpdateActions {
		switch action {
		case slurmv1alpha1.UpdateActionReconfigure:
			err = r.reconfigureCluster(ctx)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("reconfiguring Slurm cluster: %w", err)
			}
		default:
			return ctrl.Result{}, reconcile.TerminalError(fmt.Errorf("unexpected update action %s: %w", action, err))
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

	// Save config hash in ConfigMap annotation after successful reconciliation
	err = r.saveConfigHash(ctx, configMap)
	if err != nil {
		logger.V(1).Info("Failed to save config hash annotation", "error", err)
		return ctrl.Result{}, fmt.Errorf("saving config hash: %w", err)
	}

	return ctrl.Result{}, nil
}

func (r *JailedConfigReconciler) reconcileWithAggregation(ctx context.Context, jailedConfig *slurmv1alpha1.JailedConfig, aggregationKey string) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	// Get all JailedConfigs with the same aggregation key in the same namespace
	jailedConfigs := &slurmv1alpha1.JailedConfigList{}
	err := r.Client.List(ctx, jailedConfigs,
		client.InNamespace(jailedConfig.Namespace),
		client.MatchingLabels{consts.LabelJailedAggregationKey: aggregationKey},
	)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("listing JailedConfigs with aggregation key %q: %w", aggregationKey, err)
	}

	logger.V(1).Info("Found JailedConfigs for aggregation", "count", len(jailedConfigs.Items), "aggregationKey", aggregationKey)

	needsReconcile, err := r.shouldAggregatedReconciliation(ctx, jailedConfigs.Items)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("checking if aggregated reconciliation can be skipped: %w", err)
	}
	if !needsReconcile {
		logger.V(1).Info("Skipping aggregated reconciliation: all config hashes unchanged and all conditions are met", "aggregationKey", aggregationKey)
		return ctrl.Result{}, nil
	}

	for i := range jailedConfigs.Items {
		config := &jailedConfigs.Items[i]
		err := r.shouldInitializeConditions(ctx, config)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("initializing conditions for %s/%s: %w", config.Namespace, config.Name, err)
		}

		err = r.setConditions(
			ctx,
			config,
			metav1.Condition{
				Type:    string(slurmv1alpha1.FilesWritten),
				Status:  metav1.ConditionFalse,
				Reason:  slurmv1alpha1.ReasonRefresh,
				Message: "Refreshing files in jail FS (aggregated)",
			},
			metav1.Condition{
				Type:    string(slurmv1alpha1.UpdateActionsCompleted),
				Status:  metav1.ConditionFalse,
				Reason:  slurmv1alpha1.ReasonRefresh,
				Message: "Refreshing files in jail FS (aggregated)",
			},
		)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("setting conditions for %s/%s: %w", config.Namespace, config.Name, err)
		}
	}

	var totalFilesCount int
	filesBatch := NewReplacedFilesBatch(r.fs)
	defer func() {
		err = errors.Join(err, filesBatch.Cleanup())
	}()

	for i := range jailedConfigs.Items {
		config := &jailedConfigs.Items[i]

		configMap := &corev1.ConfigMap{}
		err = r.Client.Get(ctx, types.NamespacedName{
			Name:      config.Spec.ConfigMap.Name,
			Namespace: config.Namespace,
		}, configMap)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("getting ConfigMap %s for %s/%s: %w", config.Spec.ConfigMap.Name, config.Namespace, config.Name, err)
		}

		defaultMode := config.Spec.DefaultMode
		if defaultMode == nil {
			defaultMode = ptr.To(slurmv1alpha1.DefaultMode)
		}

		jailPayload, err := makePayload(config.Spec.Items, configMap, defaultMode)
		if err != nil {
			return ctrl.Result{}, reconcile.TerminalError(fmt.Errorf("making JailedConfig payload for %s/%s: %w", config.Namespace, config.Name, err))
		}

		for path := range jailPayload {
			if err := validatePayloadPath(path); err != nil {
				return ctrl.Result{}, reconcile.TerminalError(fmt.Errorf("invalid config path %q in %s/%s: %w", path, config.Namespace, config.Name, err))
			}
		}

		totalFilesCount += len(jailPayload)

		for path, payload := range jailPayload {
			err = filesBatch.Replace(path, payload.Data, os.FileMode(payload.Mode))
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("replacing file %q for %s/%s: %w", path, config.Namespace, config.Name, err)
			}
		}
	}

	logger.V(1).Info("Going to write files for aggregated group", "totalFiles", totalFilesCount, "configs", len(jailedConfigs.Items))

	for i := range jailedConfigs.Items {
		config := &jailedConfigs.Items[i]
		err = r.setConditions(
			ctx,
			config,
			metav1.Condition{
				Type:    string(slurmv1alpha1.FilesWritten),
				Status:  metav1.ConditionTrue,
				Reason:  slurmv1alpha1.ReasonSuccess,
				Message: "Files were written to jail FS (aggregated)",
			},
		)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("setting files written condition for %s/%s: %w", config.Namespace, config.Name, err)
		}
	}

	// Finish writing all files to disk
	err = filesBatch.Finish()
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("finishing replacing files in FS: %w", err)
	}

	logger.V(1).Info("Finished syncing caches for written files (aggregated)")

	needsReconfigure := false
	for i := range jailedConfigs.Items {
		config := &jailedConfigs.Items[i]
		for _, action := range config.Spec.UpdateActions {
			if action == slurmv1alpha1.UpdateActionReconfigure {
				needsReconfigure = true
				break
			}
		}
		if needsReconfigure {
			break
		}
	}

	if needsReconfigure {
		logger.V(1).Info("Performing reconfigure for aggregated group", "aggregationKey", aggregationKey)
		err = r.reconfigureCluster(ctx)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("reconfiguring Slurm cluster for aggregated group: %w", err)
		}
	}

	for i := range jailedConfigs.Items {
		config := &jailedConfigs.Items[i]
		err = r.setConditions(
			ctx,
			config,
			metav1.Condition{
				Type:    string(slurmv1alpha1.UpdateActionsCompleted),
				Status:  metav1.ConditionTrue,
				Reason:  slurmv1alpha1.ReasonSuccess,
				Message: "Update actions were called successfully (aggregated)",
			},
		)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("setting update actions completed condition for %s/%s: %w", config.Namespace, config.Name, err)
		}
	}

	logger.V(1).Info("Completed aggregated reconciliation", "aggregationKey", aggregationKey, "configs", len(jailedConfigs.Items))

	// Save hashes of all ConfigMaps after successful aggregated reconciliation
	for i := range jailedConfigs.Items {
		config := &jailedConfigs.Items[i]

		configMap := &corev1.ConfigMap{}
		err = r.Client.Get(ctx, types.NamespacedName{
			Name:      config.Spec.ConfigMap.Name,
			Namespace: config.Namespace,
		}, configMap)
		if err != nil {
			logger.V(1).Info("Failed to get ConfigMap for hash update", "configMap", config.Spec.ConfigMap.Name, "error", err)
			continue
		}

		err = r.saveConfigHash(ctx, configMap)
		if err != nil {
			logger.V(1).Info("Failed to save config hash annotation for aggregated config", "configMap", config.Spec.ConfigMap.Name, "error", err)
			return ctrl.Result{}, fmt.Errorf("saving config hash for %s/%s: %w", config.Namespace, config.Name, err)
		}
	}

	return ctrl.Result{}, nil
}

func (r *JailedConfigReconciler) shouldInitializeConditions(ctx context.Context, jailedConfig *slurmv1alpha1.JailedConfig) error {
	conditions := jailedConfig.Status.Conditions
	if len(conditions) == 0 {
		err := r.initializeConditions(ctx, jailedConfig)
		if err != nil {
			return fmt.Errorf("initializing conditions: %w", err)
		}
	}

	filesWrittenCondition := meta.FindStatusCondition(conditions, string(slurmv1alpha1.FilesWritten))
	updateActionsCondition := meta.FindStatusCondition(conditions, string(slurmv1alpha1.UpdateActionsCompleted))

	if filesWrittenCondition == nil {
		r.initializeCondition(&jailedConfig.Status, slurmv1alpha1.FilesWritten)
	}
	if updateActionsCondition == nil {
		r.initializeCondition(&jailedConfig.Status, slurmv1alpha1.UpdateActionsCompleted)
	}
	return nil

}

func (r *JailedConfigReconciler) shouldReconciliation(
	ctx context.Context, jailedConfig *slurmv1alpha1.JailedConfig, configMap *corev1.ConfigMap) (bool, error) {
	err := r.Client.Get(ctx, types.NamespacedName{
		Name:      jailedConfig.Spec.ConfigMap.Name,
		Namespace: jailedConfig.Namespace,
	}, configMap)
	if err != nil {
		if apierrors.IsNotFound(err) {
			return false, fmt.Errorf("ConfigMap %s/%s not found: %w", jailedConfig.Namespace, jailedConfig.Spec.ConfigMap.Name, err)
		}
		return false, fmt.Errorf("getting ConfigMap %s/%s: %w", jailedConfig.Namespace, jailedConfig.Spec.ConfigMap.Name, err)
	}
	return r.needsReconciliation(jailedConfig, configMap), nil
}

func NewJailedConfigReconciler(
	client client.Client,
	scheme *runtime.Scheme,
	slurmAPIClient slurmapi.Client,
	fs Fs,
	reconfigurePollInterval time.Duration,
	reconfigureWaitTimeout time.Duration,
) *JailedConfigReconciler {
	if reconfigurePollInterval == 0 {
		reconfigurePollInterval = defaultReconfigurePollInterval
	}
	if reconfigureWaitTimeout == 0 {
		reconfigureWaitTimeout = defaultReconfigureWaitTimeout
	}
	return &JailedConfigReconciler{
		Client:                  client,
		Scheme:                  scheme,
		slurmAPIClient:          slurmAPIClient,
		fs:                      fs,
		reconfigurePollInterval: reconfigurePollInterval,
		reconfigureWaitTimeout:  reconfigureWaitTimeout,
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *JailedConfigReconciler) SetupWithManager(mgr ctrl.Manager, maxConcurrency int, cacheSyncTimeout time.Duration) error {
	if r.clock == nil {
		r.clock = realClock{}
	}
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &slurmv1alpha1.JailedConfig{}, configMapField, func(rawObj client.Object) []string {
		jailedConfig := rawObj.(*slurmv1alpha1.JailedConfig)
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
		WithOptions(controllerOptions(maxConcurrency, cacheSyncTimeout)).
		Complete(r)
}

func controllerOptions(maxConcurrency int, cacheSyncTimeout time.Duration) controller.Options {
	rateLimiters := workqueue.NewTypedItemExponentialFailureRateLimiter[reconcile.Request](15*time.Second, 1*time.Minute)
	var optionsInit sync.Once
	var defaultOptions *controller.Options
	optionsInit.Do(func() {
		defaultOptions = &controller.Options{
			RateLimiter:             rateLimiters,
			CacheSyncTimeout:        cacheSyncTimeout,
			MaxConcurrentReconciles: maxConcurrency,
		}
	})
	return *defaultOptions
}

func (r *JailedConfigReconciler) findObjectsForConfigMap(ctx context.Context, configMapObject client.Object) []reconcile.Request {
	jailedConfigs := &slurmv1alpha1.JailedConfigList{}
	listOps := &client.ListOptions{
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

func (r *JailedConfigReconciler) pollSlurmNodesRestart(ctx context.Context, nodeToStartBefore map[string]int64) error {
	logger := logf.FromContext(ctx)

	for {
		logger.V(1).Info("Checking nodes start times after reconfigure", logfield.JailedConfigNodesLeft, len(nodeToStartBefore))

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
				// Start time increased, assuming nodes has restarted and picker up new config
				delete(nodeToStartBefore, name)
			}
		}

		if len(nodeToStartBefore) == 0 {
			break
		}

		select {
		case <-ctx.Done():
			return context.DeadlineExceeded
		case <-r.clock.After(r.reconfigurePollInterval):
			// Do nothing and loop
		}
	}

	return nil
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

	logger.V(1).Info("Reconfiguring cluster")

	logger.V(1).Info("Storing nodes start times before reconfigure")
	nodeToStartBefore, err := r.getNodesStartTime(ctx)
	if err != nil {
		return err
	}

	logger.V(1).Info("Requesting Slurm API to reconfigure nodes")
	reconfigureResponse, err := r.slurmAPIClient.SlurmV0041GetReconfigureWithResponse(ctx)
	if err != nil {
		return fmt.Errorf("requesting Slurm API to reconfigure nodes: %w", err)
	}
	if err = checkStatus(reconfigureResponse); err != nil {
		return fmt.Errorf("reconfigure via Slurm API: %w", err)
	}
	if err = checkApiErrors(reconfigureResponse.JSON200.Errors); err != nil {
		return fmt.Errorf("reconfigure via Slurm API: %w", err)
	}

	pollCtx, cancel := context.WithTimeout(ctx, r.reconfigureWaitTimeout)
	defer cancel()
	err = r.pollSlurmNodesRestart(pollCtx, nodeToStartBefore)
	if errors.Is(err, context.DeadlineExceeded) {
		return fmt.Errorf("nodes did not restart: %w", err)
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
	if meta.FindStatusCondition(status.Conditions, string(cond)) != nil {
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

func (r *JailedConfigReconciler) calculateConfigHash(configMap *corev1.ConfigMap) string {
	// Create a simple hash from all ConfigMap data
	var hashData strings.Builder

	// Add all Data as a single string
	hashData.WriteString(fmt.Sprintf("data:%+v;", configMap.Data))

	// Add all BinaryData as a single string
	hashData.WriteString(fmt.Sprintf("binarydata:%+v;", configMap.BinaryData))

	hash := sha256.Sum256([]byte(hashData.String()))
	return hex.EncodeToString(hash[:])
}

// needsReconciliation checks if reconciliation is needed
// Returns true if reconciliation should proceed, false if it can be skipped
func (r *JailedConfigReconciler) needsReconciliation(jailedConfig *slurmv1alpha1.JailedConfig, configMap *corev1.ConfigMap) bool {
	filesWrittenCondition := meta.FindStatusCondition(jailedConfig.Status.Conditions, string(slurmv1alpha1.FilesWritten))
	updateActionsCondition := meta.FindStatusCondition(jailedConfig.Status.Conditions, string(slurmv1alpha1.UpdateActionsCompleted))

	if filesWrittenCondition == nil || filesWrittenCondition.Status != metav1.ConditionTrue {
		return true
	}

	if updateActionsCondition == nil || updateActionsCondition.Status != metav1.ConditionTrue {
		return true
	}
	currentHash := r.calculateConfigHash(configMap)

	if configMap.Annotations == nil {
		return true
	}

	savedHash, exists := configMap.Annotations[consts.AnnotationConfigHash]
	if !exists {
		return true
	}

	return currentHash != savedHash
}

// saveConfigHash saves config hash in ConfigMap annotation
func (r *JailedConfigReconciler) saveConfigHash(ctx context.Context, configMap *corev1.ConfigMap) error {
	currentHash := r.calculateConfigHash(configMap)

	if configMap.Annotations == nil {
		configMap.Annotations = make(map[string]string)
	}
	if savedHash, exists := configMap.Annotations[consts.AnnotationConfigHash]; exists && savedHash == currentHash {
		return nil
	}

	patch := client.MergeFrom(configMap.DeepCopy())
	configMap.Annotations[consts.AnnotationConfigHash] = currentHash

	return r.Client.Patch(ctx, configMap, patch)
}

// shouldAggregatedReconciliation checks if aggregated reconciliation is needed
// Returns true if reconciliation should proceed, false if it can be skipped
func (r *JailedConfigReconciler) shouldAggregatedReconciliation(ctx context.Context, jailedConfigs []slurmv1alpha1.JailedConfig) (bool, error) {
	for i := range jailedConfigs {
		config := &jailedConfigs[i]

		filesWrittenCondition := meta.FindStatusCondition(config.Status.Conditions, string(slurmv1alpha1.FilesWritten))
		updateActionsCondition := meta.FindStatusCondition(config.Status.Conditions, string(slurmv1alpha1.UpdateActionsCompleted))

		if filesWrittenCondition == nil || filesWrittenCondition.Status != metav1.ConditionTrue {
			return true, nil
		}

		if updateActionsCondition == nil || updateActionsCondition.Status != metav1.ConditionTrue {
			return true, nil
		}
	}

	for i := range jailedConfigs {
		config := &jailedConfigs[i]

		configMap := &corev1.ConfigMap{}
		err := r.Client.Get(ctx, types.NamespacedName{
			Name:      config.Spec.ConfigMap.Name,
			Namespace: config.Namespace,
		}, configMap)
		if err != nil {
			return false, err
		}

		if r.needsReconciliation(config, configMap) {
			return true, nil
		}
	}

	return false, nil
}
