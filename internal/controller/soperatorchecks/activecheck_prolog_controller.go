package soperatorchecks

import (
	"context"
	"path/filepath"
	"time"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	"k8s.io/utils/ptr"
	slurmv1 "nebius.ai/slurm-operator/api/v1"
	"nebius.ai/slurm-operator/internal/consts"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"

	"nebius.ai/slurm-operator/api/v1alpha1"
	"nebius.ai/slurm-operator/internal/controller/reconciler"
	"nebius.ai/slurm-operator/internal/controllerconfig"
)

var (
	SlurmActiveCheckPrologControllerName = "soperatorchecks.activecheckprolog"
	DefaultRequeueResult                 = ctrl.Result{
		RequeueAfter: 3 * time.Minute,
		Requeue:      true,
	}
)

type ActiveCheckPrologReconciler struct {
	*reconciler.Reconciler
	reconcileTimeout time.Duration
}

func NewActiveCheckPrologController(
	client client.Client,
	scheme *runtime.Scheme,
	recorder record.EventRecorder,
	reconcileTimeout time.Duration,
) *ActiveCheckPrologReconciler {
	r := reconciler.NewReconciler(client, scheme, recorder)

	return &ActiveCheckPrologReconciler{
		Reconciler:       r,
		reconcileTimeout: reconcileTimeout,
	}
}

func (r *ActiveCheckPrologReconciler) SetupWithManager(mgr ctrl.Manager,
	maxConcurrency int, cacheSyncTimeout time.Duration) error {
	return ctrl.NewControllerManagedBy(mgr).Named(SlurmActiveCheckPrologControllerName).
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

// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=slurmclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;update;create;patch

// Reconcile reconciles all resources necessary for active checks controller
func (r *ActiveCheckPrologReconciler) Reconcile(
	ctx context.Context,
	req ctrl.Request,
) (ctrl.Result, error) {
	logger := log.FromContext(ctx).WithName(SlurmActiveCheckPrologControllerName)
	logger.Info(
		"Starting reconciliation", "SlurmCluster", req.Name, "Namespace", req.Namespace,
	)

	if err := r.updatePrologConfigMap(ctx, req.Namespace, r.getPrologScript()); err != nil {
		logger.Error(err, "Failed to update ConfigMap with active check prolog script")
		return DefaultRequeueResult, nil
	}

	logger.Info("Reconciliation completed successfully")
	return DefaultRequeueResult, nil
}

func (r *ActiveCheckPrologReconciler) updatePrologConfigMap(ctx context.Context, namespace string, config string) error {
	configMap := &corev1.ConfigMap{
		TypeMeta: ctrl.TypeMeta{
			APIVersion: corev1.SchemeGroupVersion.Version,
			Kind:       "ConfigMap",
		},
		ObjectMeta: ctrl.ObjectMeta{
			Name:      consts.ConfigMapNameActiveCheckPrologScript,
			Namespace: namespace,
		},
		Data: map[string]string{
			consts.ConfigMapKeyActiveCheckPrologScript: config,
		},
	}

	err := r.Client.Patch(ctx, configMap, client.Apply,
		client.ForceOwnership, client.FieldOwner(SlurmActiveCheckPrologControllerName))

	if err != nil {
		return err
	}

	jailedConfig := &v1alpha1.JailedConfig{
		TypeMeta: ctrl.TypeMeta{
			APIVersion: v1alpha1.GroupVersion.Version,
			Kind:       "JailedConfig",
		},
		ObjectMeta: ctrl.ObjectMeta{
			Name:      consts.ConfigMapNameActiveCheckPrologScript,
			Namespace: namespace,
		},
		Spec: v1alpha1.JailedConfigSpec{
			ConfigMap: v1alpha1.ConfigMapReference{
				Name: consts.ConfigMapNameActiveCheckPrologScript,
			},
			Items: []corev1.KeyToPath{
				{
					Key:  consts.ConfigMapKeyActiveCheckPrologScript,
					Path: filepath.Join("/etc/slurm/", consts.ConfigMapKeyActiveCheckPrologScript),
					Mode: ptr.To(int32(0o755)),
				},
			},
		},
	}

	err = r.Client.Patch(ctx, jailedConfig, client.Apply,
		client.ForceOwnership, client.FieldOwner(SlurmActiveCheckPrologControllerName))

	if err != nil {
		return err
	}

	return nil
}

func (r *ActiveCheckPrologReconciler) getPrologScript() string {
	return `#!/bin/bash

ACTIVE_CHECK_NAME="$SLURM_JOB_NAME"
echo "Active check name: $ACTIVE_CHECK_NAME"

NODE_NAME=$(hostname)
echo "Running embedded prolog on node: $NODE_NAME"

extra_json=$(scontrol show node "$NODE_NAME" | awk -F= '/Extra=/{print $2}')
if [[ -z "$extra_json" || "$extra_json" == "none" ]]; then
    extra_json="{}"
fi

updated_json=$(echo "$extra_json" | jq -c --arg key "$ACTIVE_CHECK_NAME" 'del(.[$key])')

sudo scontrol update NodeName="$NODE_NAME" Extra="$updated_json"

echo "prolog completed for $NODE_NAME"`
}
