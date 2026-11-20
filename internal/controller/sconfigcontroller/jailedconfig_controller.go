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
	"os"
	"slices"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/events"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	v0044 "github.com/SlinkyProject/slurm-client/api/v0044"

	slurmv1alpha1 "nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/controllerconfig"
	"nebius.ai/slurm-operator/internal/logfield"
	"nebius.ai/slurm-operator/internal/slurmapi"
	"nebius.ai/slurm-operator/internal/utils/jailedpayload"
)

// ControllerName is the controller-runtime name of the JailedConfig reconciler.
const ControllerName = "jailedconfig"

const (
	configMapField = ".spec.configMap.name"

	defaultReconfigureWaitTimeout  = 5 * time.Minute
	defaultReconfigurePollInterval = 20 * time.Second
)

// JailedConfigReconciler reconciles a JailedConfig object
type JailedConfigReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	clusterName             string
	recorder                events.EventRecorder
	slurmAPIClient          slurmapi.Client
	clock                   Clock
	fs                      Fs
	reconfigurePollInterval time.Duration
	reconfigureWaitTimeout  time.Duration
}

// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=jailedconfigs,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=jailedconfigs/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=jailedconfigs/finalizers,verbs=update
// +kubebuilder:rbac:groups="core",resources=configmaps,verbs=get;list;watch
// +kubebuilder:rbac:groups="events.k8s.io",resources=events,verbs=create;patch

// Clock is used to fake timing for testing
type Clock interface {
	After(duration time.Duration) <-chan time.Time
}

type realClock struct{}

var _ Clock = realClock{}

func (_ realClock) After(duration time.Duration) <-chan time.Time { return time.After(duration) }

// writtenPayload is what one reconciliation wrote for a JailedConfig: the payload hash, and the
// generation and update actions of the spec the payload was rendered from.
//
// The spec is captured when the payload is rendered, not later: every status patch refreshes the
// object from the API server, so by the time a reconfigure completes the object may already carry
// a spec this reconciliation never read.
type writtenPayload struct {
	hash          string
	generation    int64
	updateActions []slurmv1alpha1.UpdateAction
}

func newWrittenPayload(jailedConfig *slurmv1alpha1.JailedConfig, payload map[string]jailedpayload.File) writtenPayload {
	return writtenPayload{
		hash:          jailedpayload.Hash(payload),
		generation:    jailedConfig.Generation,
		updateActions: slices.Clone(jailedConfig.Spec.UpdateActions),
	}
}

func (w writtenPayload) reconfigureRequested() bool {
	return slices.Contains(w.updateActions, slurmv1alpha1.UpdateActionReconfigure)
}

// revokeStaleReconfigure revokes a ReconfigurePerformed confirmation the payload is about to make
// stale. It must run before the payload replaces any file.
//
// A confirmation is read together with AppliedHash as "slurmctld re-read this content", which stops
// being true the moment the file changes. Left in place while the new content is reconfigured, it
// confirms the previous content: a topology going A -> B -> A during that reconfigure is then taken
// as re-read and its request withdrawn, while slurmctld loads B.
func (r *JailedConfigReconciler) revokeStaleReconfigure(ctx context.Context, jailedConfig *slurmv1alpha1.JailedConfig, written writtenPayload) error {
	if written.hash == jailedConfig.Status.AppliedHash {
		return nil
	}
	confirmation := meta.FindStatusCondition(jailedConfig.Status.Conditions, string(slurmv1alpha1.ReconfigurePerformed))
	if confirmation == nil || confirmation.Status != metav1.ConditionTrue {
		return nil
	}
	return r.setConditions(ctx, jailedConfig, metav1.Condition{
		Type:               string(slurmv1alpha1.ReconfigurePerformed),
		Status:             metav1.ConditionFalse,
		Reason:             slurmv1alpha1.ReasonRefresh,
		Message:            "Files are changing, Slurm was not reconfigured for them yet",
		ObservedGeneration: written.generation,
	})
}

// setAppliedHash records the payload as applied on a pass that ran no reconfigure; after one,
// setReconfigurePerformed has recorded it already. It runs only once the files are written and the
// update actions are settled, so a failure is retried instead of being marked done.
func (r *JailedConfigReconciler) setAppliedHash(ctx context.Context, jailedConfig *slurmv1alpha1.JailedConfig, hash string) error {
	if jailedConfig.Status.AppliedHash == hash {
		return nil
	}
	return r.patchStatus(ctx, jailedConfig, func(status *slurmv1alpha1.JailedConfigStatus) {
		status.AppliedHash = hash
	})
}

// setReconfigurePerformed records a completed reconfigure and the payload it read in one status
// patch, along with any other conditions given.
//
// Both must land together. Patched apart, a requester watching the status sees ReconfigurePerformed
// already current while AppliedHash still names the payload before it. When the published content
// has meanwhile returned to that earlier payload -- a topology going A -> unknown -> A while the
// reconfigure for unknown runs -- the requester takes this reconfigure as one over the newer content
// and withdraws a request Slurm never satisfied.
func (r *JailedConfigReconciler) setReconfigurePerformed(
	ctx context.Context,
	jailedConfig *slurmv1alpha1.JailedConfig,
	written writtenPayload,
	conditions ...metav1.Condition,
) error {
	conditions = append(conditions, metav1.Condition{
		Type:               string(slurmv1alpha1.ReconfigurePerformed),
		Status:             metav1.ConditionTrue,
		Reason:             slurmv1alpha1.ReasonSuccess,
		Message:            "Slurm was reconfigured for this generation",
		ObservedGeneration: written.generation,
	})
	return r.patchStatus(ctx, jailedConfig, func(status *slurmv1alpha1.JailedConfigStatus) {
		applyConditions(status, conditions...)
		status.AppliedHash = written.hash
	})
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

	if jailedConfig.Labels[consts.LabelInstanceKey] != r.clusterName {
		return ctrl.Result{}, nil
	}

	err := r.shouldInitializeConditions(ctx, jailedConfig)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("initializing conditions: %w", err)
	}

	configMap := &corev1.ConfigMap{}
	err = r.Client.Get(ctx, types.NamespacedName{
		Name:      jailedConfig.Spec.ConfigMap.Name,
		Namespace: jailedConfig.Namespace,
	}, configMap)
	if err != nil {
		if apierrors.IsNotFound(err) {
			// ConfigMap not found, so it must have been deleted or never existed
			// When ConfigMap would be created, it would trigger reconciliation of JailedConfig,
			// so we can just skip this reconciliation and wait for next one
			logger.Info(fmt.Sprintf("ConfigMap %s not found, skipping JailedConfig reconciliation", jailedConfig.Spec.ConfigMap.Name))
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("getting ConfigMap %s: %w", jailedConfig.Spec.ConfigMap.Name, err)
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

	jailPayload, err := jailedpayload.Make(&jailedConfig.Spec, configMap)
	if err != nil {
		return ctrl.Result{}, reconcile.TerminalError(fmt.Errorf("making JailedConfig payload: %w", err))
	}
	written := newWrittenPayload(jailedConfig, jailPayload)
	if err := r.revokeStaleReconfigure(ctx, jailedConfig, written); err != nil {
		return ctrl.Result{}, fmt.Errorf("revoking stale reconfigure confirmation: %w", err)
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

	if len(written.updateActions) == 0 {
		logger.V(1).Info("No update actions specified, skipping further processing")
		err = r.setConditions(
			ctx,
			jailedConfig,
			metav1.Condition{
				Type:    string(slurmv1alpha1.UpdateActionsCompleted),
				Status:  metav1.ConditionTrue,
				Reason:  slurmv1alpha1.ReasonMissingAction,
				Message: "No update actions specified, skipping further processing",
			},
		)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("setting conditions: %w", err)
		}
	} else if reconfigureIsDue(jailedConfig, written) {
		err = r.reconfigureCluster(ctx)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("reconfiguring Slurm cluster: %w", err)
		}
		err = r.setReconfigurePerformed(
			ctx,
			jailedConfig,
			written,
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
	} else {
		logger.V(1).Info("Nothing to reconfigure for this config")
		err = r.setConditions(
			ctx,
			jailedConfig,
			metav1.Condition{
				Type:    string(slurmv1alpha1.UpdateActionsCompleted),
				Status:  metav1.ConditionTrue,
				Reason:  slurmv1alpha1.ReasonSuccess,
				Message: "Update actions were not called because no reconfigure was needed",
			},
		)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("setting conditions: %w", err)
		}
	}

	if err := r.setAppliedHash(ctx, jailedConfig, written.hash); err != nil {
		return ctrl.Result{}, fmt.Errorf("recording applied hash: %w", err)
	}

	return ctrl.Result{}, nil
}

func (r *JailedConfigReconciler) reconcileWithAggregation(ctx context.Context, jailedConfig *slurmv1alpha1.JailedConfig, aggregationKey string) (ctrl.Result, error) {
	logger := logf.FromContext(ctx)

	// Get all JailedConfigs with the same aggregation key and cluster name
	jailedConfigs := &slurmv1alpha1.JailedConfigList{}
	err := r.Client.List(ctx, jailedConfigs,
		client.InNamespace(jailedConfig.Namespace),
		client.MatchingLabels{
			consts.LabelJailedAggregationKey: aggregationKey,
			consts.LabelInstanceKey:          r.clusterName,
		},
	)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("listing JailedConfigs with aggregation key %q: %w", aggregationKey, err)
	}

	logger.V(1).Info("Found JailedConfigs for aggregation", "count", len(jailedConfigs.Items), "aggregationKey", aggregationKey)

	for i := range jailedConfigs.Items {
		jailedConfig := &jailedConfigs.Items[i]
		err := r.shouldInitializeConditions(ctx, jailedConfig)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("initializing conditions for %s/%s: %w", jailedConfig.Namespace, jailedConfig.Name, err)
		}

		err = r.setConditions(
			ctx,
			jailedConfig,
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
			return ctrl.Result{}, fmt.Errorf("setting conditions for %s/%s: %w", jailedConfig.Namespace, jailedConfig.Name, err)
		}
	}

	// Payloads of the configs whose files were written this pass, used below to tell a real change
	// from a re-run over identical content.
	writtenPayloads := make(map[string]writtenPayload, len(jailedConfigs.Items))

	var totalFilesCount int
	filesBatch := NewReplacedFilesBatch(r.fs)
	defer func() {
		err = errors.Join(err, filesBatch.Cleanup())
	}()

	for i := range jailedConfigs.Items {
		jailedConfig := &jailedConfigs.Items[i]

		configMap := &corev1.ConfigMap{}
		err = r.Client.Get(ctx, types.NamespacedName{
			Name:      jailedConfig.Spec.ConfigMap.Name,
			Namespace: jailedConfig.Namespace,
		}, configMap)
		if err != nil {
			if apierrors.IsNotFound(err) {
				// ConfigMap not found, so it must have been deleted or never existed
				// When ConfigMap would be created, it would trigger reconciliation of JailedConfig,
				// so we can just skip this reconciliation and wait for next one
				logger.Info(fmt.Sprintf("ConfigMap %s not found, skipping JailedConfig reconciliation", jailedConfig.Spec.ConfigMap.Name))
				err = r.setConditions(
					ctx,
					jailedConfig,
					metav1.Condition{
						Type:    string(slurmv1alpha1.FilesWritten),
						Status:  metav1.ConditionFalse,
						Reason:  slurmv1alpha1.ReasonNotFound,
						Message: "ConfigMap not found, files were not written (aggregated)",
					},
				)
				if err != nil {
					return ctrl.Result{}, fmt.Errorf("setting files written condition for %s/%s: %w", jailedConfig.Namespace, jailedConfig.Name, err)
				}
				continue
			}
			return ctrl.Result{}, fmt.Errorf("getting ConfigMap %s for %s/%s: %w", jailedConfig.Spec.ConfigMap.Name, jailedConfig.Namespace, jailedConfig.Name, err)
		}

		jailPayload, err := jailedpayload.Make(&jailedConfig.Spec, configMap)
		if err != nil {
			return ctrl.Result{}, reconcile.TerminalError(fmt.Errorf("making JailedConfig payload for %s/%s: %w", jailedConfig.Namespace, jailedConfig.Name, err))
		}

		totalFilesCount += len(jailPayload)
		written := newWrittenPayload(jailedConfig, jailPayload)
		if err := r.revokeStaleReconfigure(ctx, jailedConfig, written); err != nil {
			return ctrl.Result{}, fmt.Errorf("revoking stale reconfigure confirmation for %s/%s: %w", jailedConfig.Namespace, jailedConfig.Name, err)
		}
		writtenPayloads[jailedConfig.Name] = written

		for path, payload := range jailPayload {
			err = filesBatch.Replace(path, payload.Data, os.FileMode(payload.Mode))
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("replacing file %q for %s/%s: %w", path, jailedConfig.Namespace, jailedConfig.Name, err)
			}
		}
		logger.V(1).Info("Done writing files for JailedConfig", "name", jailedConfig.Name, "filesCount", i+1, "totalFilesCount", totalFilesCount)
		err = r.setConditions(
			ctx,
			jailedConfig,
			metav1.Condition{
				Type:    string(slurmv1alpha1.FilesWritten),
				Status:  metav1.ConditionTrue,
				Reason:  slurmv1alpha1.ReasonSuccess,
				Message: "Files were written to jail FS (aggregated)",
			},
		)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("setting conditions: %w", err)
		}
	}

	logger.V(1).Info("Going to write files for aggregated group", "totalFiles", totalFilesCount, "configs", len(jailedConfigs.Items))
	// Finish writing all files to disk
	err = filesBatch.Finish()
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("finishing replacing files in FS: %w", err)
	}

	logger.V(1).Info("Finished syncing caches for written files (aggregated)")

	// Reconfigure only for a config that both asks for it and actually changed. Without the change
	// check the group reconfigures on every pass, because the slurm configs member carries the
	// action permanently -- so a topology-only edit would restart slurmd across the cluster.
	needsReconfigure := false
	for i := range jailedConfigs.Items {
		jailedConfig := &jailedConfigs.Items[i]
		written, isWritten := writtenPayloads[jailedConfig.Name]
		if !isWritten {
			continue
		}
		if reconfigureIsDue(jailedConfig, written) {
			needsReconfigure = true
			break
		}
	}

	if needsReconfigure {
		logger.V(1).Info("Performing reconfigure for aggregated group", "aggregationKey", aggregationKey)
		if err = r.reconfigureCluster(ctx); err != nil {
			r.recordReconfigure(jailedConfigs.Items, writtenPayloads, corev1.EventTypeWarning,
				reasonReconfigureFailed, fmt.Sprintf("Slurm reconfigure failed: %v", err))
			return ctrl.Result{}, fmt.Errorf("reconfiguring Slurm cluster for aggregated group: %w", err)
		}
		r.recordReconfigure(jailedConfigs.Items, writtenPayloads, corev1.EventTypeNormal,
			reasonReconfigured, "slurmctld re-read the configs written for this JailedConfig")

		// Report it against the generation each config's payload was rendered from, so whoever asked
		// for the action can tell this reconfigure from one that ran for another version of the spec.
		for i := range jailedConfigs.Items {
			jailedConfig := &jailedConfigs.Items[i]
			// A member whose ConfigMap was missing wrote nothing, so this reconfigure says nothing
			// about it. Confirming it anyway would let its requester withdraw a request that was
			// never satisfied.
			written, isWritten := writtenPayloads[jailedConfig.Name]
			if !isWritten {
				continue
			}
			err = r.setReconfigurePerformed(ctx, jailedConfig, written)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("setting reconfigure condition for %s/%s: %w", jailedConfig.Namespace, jailedConfig.Name, err)
			}
		}
	}

	// Set UpdateActionsCompleted condition for all configs based on whether reconfigure was needed and files were written
	err = r.setUpdateActionsCompletedForConfigs(ctx, jailedConfigs.Items, needsReconfigure)
	if err != nil {
		return ctrl.Result{}, err
	}

	// Every file the group just wrote is recorded as applied. After a reconfigure this is already
	// done alongside ReconfigurePerformed, so here it only covers a pass that needed none.
	for i := range jailedConfigs.Items {
		jailedConfig := &jailedConfigs.Items[i]
		written, isWritten := writtenPayloads[jailedConfig.Name]
		if !isWritten {
			continue
		}
		if err := r.setAppliedHash(ctx, jailedConfig, written.hash); err != nil {
			return ctrl.Result{}, fmt.Errorf("recording applied hash for %s/%s: %w", jailedConfig.Namespace, jailedConfig.Name, err)
		}
	}

	logger.V(1).Info("Completed aggregated reconciliation", "aggregationKey", aggregationKey, "configs", len(jailedConfigs.Items))

	return ctrl.Result{}, nil
}

// reconfigureIsDue reports whether this config needs slurmctld to re-read what was just written.
//
// It is due when the content changed, and also when the request itself is new: a config can ask for
// a reconfigure over content that renders identically -- a topology gaining an entry that reaches no
// node yet, say -- and gating on the hash alone would leave that request permanently unsatisfied.
//
// The request and its generation come from the spec the payload was rendered from, not from the
// object, which status patches may have refreshed with a newer spec by now.
func reconfigureIsDue(jailedConfig *slurmv1alpha1.JailedConfig, written writtenPayload) bool {
	if !written.reconfigureRequested() {
		return false
	}

	if written.hash != jailedConfig.Status.AppliedHash {
		return true
	}

	condition := meta.FindStatusCondition(jailedConfig.Status.Conditions, string(slurmv1alpha1.ReconfigurePerformed))
	return condition == nil || condition.Status != metav1.ConditionTrue ||
		condition.ObservedGeneration != written.generation
}

// hasFailedFilesWrittenCondition checks if the JailedConfig has a failed FilesWritten condition.
func hasFailedFilesWrittenCondition(jailedConfig *slurmv1alpha1.JailedConfig) bool {
	condition := meta.FindStatusCondition(jailedConfig.Status.Conditions, string(slurmv1alpha1.FilesWritten))
	return condition != nil && condition.Reason == string(slurmv1alpha1.ReasonNotFound)
}

// setUpdateActionsCompletedForConfigs sets the UpdateActionsCompleted condition for all provided configs
// based on whether reconfigure was needed and their individual FilesWritten status
func (r *JailedConfigReconciler) setUpdateActionsCompletedForConfigs(ctx context.Context, configs []slurmv1alpha1.JailedConfig, needsReconfigure bool) error {
	for i := range configs {
		jailedConfig := &configs[i]

		var condition metav1.Condition
		if needsReconfigure {
			if hasFailedFilesWrittenCondition(jailedConfig) {
				condition = metav1.Condition{
					Type:    string(slurmv1alpha1.UpdateActionsCompleted),
					Status:  metav1.ConditionFalse,
					Reason:  slurmv1alpha1.ReasonNotWritten,
					Message: "Update actions were not called because files were not written (aggregated)",
				}
			} else {
				condition = metav1.Condition{
					Type:    string(slurmv1alpha1.UpdateActionsCompleted),
					Status:  metav1.ConditionTrue,
					Reason:  slurmv1alpha1.ReasonSuccess,
					Message: "Update actions were called successfully (aggregated)",
				}
			}
		} else {
			condition = metav1.Condition{
				Type:    string(slurmv1alpha1.UpdateActionsCompleted),
				Status:  metav1.ConditionTrue,
				Reason:  slurmv1alpha1.ReasonSuccess,
				Message: "Update actions were not called because no reconfigure needed (aggregated)",
			}
		}

		err := r.setConditions(ctx, jailedConfig, condition)
		if err != nil {
			return fmt.Errorf("setting update actions completed condition for %s/%s: %w", jailedConfig.Namespace, jailedConfig.Name, err)
		}
	}
	return nil
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

func NewJailedConfigReconciler(
	client client.Client,
	scheme *runtime.Scheme,
	clusterName string,
	recorder events.EventRecorder,
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
		clusterName:             clusterName,
		recorder:                recorder,
		slurmAPIClient:          slurmAPIClient,
		fs:                      fs,
		reconfigurePollInterval: reconfigurePollInterval,
		reconfigureWaitTimeout:  reconfigureWaitTimeout,
	}
}

// Event reasons emitted against the JailedConfigs a reconfigure was performed for.
const (
	reasonReconfigured      = "Reconfigured"
	reasonReconfigureFailed = "ReconfigureFailed"

	// actionReconfigure names what the controller did, as the events API asks for.
	actionReconfigure = "Reconfigure"
)

// recordReconfigure emits one event per JailedConfig the reconfigure actually covers.
//
// Configs whose ConfigMap was missing wrote nothing, so this reconfigure says nothing about them
// and they are left out, matching how the ReconfigurePerformed condition is set.
//
// The event is what makes a reconfigure visible without reading operator logs: `kubectl describe
// jailedconfig` then shows when slurmctld last re-read a config and whether it succeeded.
func (r *JailedConfigReconciler) recordReconfigure(
	jailedConfigs []slurmv1alpha1.JailedConfig,
	writtenPayloads map[string]writtenPayload,
	eventType, reason, note string,
) {
	if r.recorder == nil {
		return
	}
	for i := range jailedConfigs {
		jailedConfig := &jailedConfigs[i]
		if _, written := writtenPayloads[jailedConfig.Name]; !written {
			continue
		}
		r.recorder.Eventf(jailedConfig, nil, eventType, reason, actionReconfigure, note)
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
		Named(ControllerName).
		WithOptions(controllerconfig.ControllerOptionsWithRateLimit(maxConcurrency, cacheSyncTimeout, 15*time.Second, 1*time.Minute)).
		Complete(controllerconfig.NamedReconciler(ControllerName, r))
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

func checkApiErrors(responseErrors *v0044.V0044OpenapiErrors) error {
	if len(*responseErrors) > 0 {
		errs := make([]error, 0)
		for _, err := range *responseErrors {
			errs = append(errs, fmt.Errorf(
				"API error %d %s, source %s",
				ptr.Deref(err.ErrorNumber, 0),
				ptr.Deref(err.Error, "unknown"),
				ptr.Deref(err.Source, "unknown"),
			))
		}
		return errors.Join(errs...)
	}

	return nil
}

func (r *JailedConfigReconciler) getNodes(ctx context.Context) (*v0044.V0044OpenapiNodesResp, error) {
	nodesBefore, err := r.slurmAPIClient.SlurmV0044GetNodesWithResponse(ctx, nil)
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

		var skip bool
		for _, state := range *node.State {
			if state == v0044.V0044NodeStateNOTRESPONDING ||
				state == v0044.V0044NodeStatePOWERINGDOWN ||
				state == v0044.V0044NodeStatePOWEREDDOWN {
				// Ignore NOT_RESPONDING, POWERING_DOWN and POWERED_DOWN nodes, since their start time doesn't change after reconfigure.
				skip = true
			}
		}
		if skip {
			continue
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

		// Remove nodes that are no longer among active (responding) nodes.
		// This can happen if the node became NOT_RESPONDING after reconfigure
		// (e.g. slurmd is not running) or was removed from Slurm entirely.
		// In either case we cannot track their start time, so stop waiting.
		for name := range nodeToStartBefore {
			if _, stillPresent := nodeToStartAfter[name]; !stillPresent {
				logger.V(1).Info("Node is no longer among active nodes, skipping", "node", name)
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
	reconfigureResponse, err := r.slurmAPIClient.SlurmV0044GetReconfigureWithResponse(ctx)
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
		applyConditions(status, conditions...)
	})
}

func applyConditions(status *slurmv1alpha1.JailedConfigStatus, conditions ...metav1.Condition) {
	for _, cond := range conditions {
		_ = meta.SetStatusCondition(&status.Conditions, cond)
	}
}
