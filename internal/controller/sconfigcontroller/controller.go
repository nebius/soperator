/*
Copyright 2024 Nebius B.V.

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
	"fmt"
	"strings"
	"time"

	"nebius.ai/slurm-operator/internal/consts"
	"nebius.ai/slurm-operator/internal/controller/reconciler"
	"nebius.ai/slurm-operator/internal/controllerconfig"
	"nebius.ai/slurm-operator/internal/slurmapi"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/tools/record"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
)

const SConfigControllerName = "sconfigcontroller"

type Store interface {
	Add(name, content, subPath string) error
	SetExecutable(name, subPath string) error
}

// SConfigControllerReconciler reconciles a SConfigController object
type ControllerReconciler struct {
	*reconciler.Reconciler
	slurmAPIClient slurmapi.Client

	fileStore Store
}

// +kubebuilder:rbac:groups=slurm.nebius.ai,resources=configmaps,verbs=get;list;watch

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the SConfigController object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.20.2/pkg/reconcile
func (r *ControllerReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)

	logger.V(1).Info("Reconciling SConfigController")

	configMapList := &corev1.ConfigMapList{}

	if err := r.List(
		ctx, configMapList, client.InNamespace(req.Namespace), client.MatchingLabels{consts.LabelSConfigControllerSourceKey: "true"},
	); err != nil {
		return ctrl.Result{}, fmt.Errorf("listing ConfigMaps: %w", err)
	}

	if len(configMapList.Items) == 0 {
		return ctrl.Result{}, nil
	}

	reconfigure := false
	for _, configMap := range configMapList.Items {
		if len(configMap.Data) == 0 {
			logger.V(1).Info("Skipping ConfigMap with no data", "name", configMap.Name)
			continue
		}
		reconfigure = true
		subPath := configMap.Annotations[consts.AnnotationSConfigControllerSourceKey]
		if err := validatePath(subPath); err != nil {
			logger.V(1).Error(err, "Invalid path in ConfigMap annotations", "path", subPath)
			return ctrl.Result{}, fmt.Errorf("invalid path %q in ConfigMap annotations: %w", subPath, err)
		}

		subPath = trimSlurmPrefix(subPath)

		executable := configMap.Annotations[consts.AnnotationSConfigControllerExecutableKey] == consts.DefaultSConfigControllerExecutableValue
		for configName, configContent := range configMap.Data {
			logger.V(1).Info("About to save slurm config", "configName", configName)

			err := r.fileStore.Add(configName, configContent, subPath)
			if err != nil {
				return ctrl.Result{}, fmt.Errorf("adding file %q to fileStore: %w", configName, err)
			}

			if executable {
				err = r.fileStore.SetExecutable(configName, subPath)
				if err != nil {
					return ctrl.Result{}, fmt.Errorf("setting executable for file %q in fileStore: %w", configName, err)
				}
			}
		}

	}
	logger.V(1).Info("Requesting Slurm API to reconfigure workers")

	if reconfigure {
		_, err := r.slurmAPIClient.SlurmV0041GetReconfigureWithResponse(ctx)
		if err != nil {
			return ctrl.Result{}, fmt.Errorf("requesting Slurm API to reconfigure workers: %w", err)
		}
		logger.V(1).Info("Slurm API reconfigure request sent successfully")
		logger.V(1).Info("Sleeping for 1 second to allow Slurm to reconfigure")
		// Default MessageTimeout for Soperator is 1 second
		time.Sleep(1 * time.Second)
	}

	return ctrl.Result{}, nil
}

func NewController(
	client client.Client,
	scheme *runtime.Scheme,
	recorder record.EventRecorder,

	slurmAPIClient slurmapi.Client,
	fileStore Store,
) *ControllerReconciler {
	r := reconciler.NewReconciler(client, scheme, recorder)

	return &ControllerReconciler{
		Reconciler:     r,
		slurmAPIClient: slurmAPIClient,
		fileStore:      fileStore,
	}
}

func validatePath(path string) error {
	switch {
	case path == "":
		return nil
	case !strings.HasPrefix(path, "/slurm"):
		return fmt.Errorf("invalid path %q: must start with '/slurm'", path)
	case strings.Contains(path, "/..") || strings.HasPrefix(path, "../"):
		return fmt.Errorf("invalid path %q: path traversal detected", path)
	default:
		return nil
	}
}

func trimSlurmPrefix(subPath string) string {
	return strings.TrimPrefix(subPath, "/slurm")
}

func (r *ControllerReconciler) SetupWithManager(mgr ctrl.Manager,
	maxConcurrency int, cacheSyncTimeout time.Duration) error {

	return ctrl.NewControllerManagedBy(mgr).
		For(&corev1.ConfigMap{}, builder.WithPredicates(predicate.Funcs{
			CreateFunc: func(e event.CreateEvent) bool {
				configMap, ok := e.Object.(*corev1.ConfigMap)
				if !ok {
					return false
				}

				return isValidConfigMap(configMap)
			},
			UpdateFunc: func(e event.UpdateEvent) bool {
				configMap, ok := e.ObjectNew.(*corev1.ConfigMap)
				if !ok {
					return false
				}

				return isValidConfigMap(configMap)
			},
			DeleteFunc: func(e event.DeleteEvent) bool {
				return false
			},
			GenericFunc: func(e event.GenericEvent) bool {
				configMap, ok := e.Object.(*corev1.ConfigMap)
				if !ok {
					return false
				}

				return isValidConfigMap(configMap)
			},
		})).
		WithOptions(controllerconfig.ControllerOptions(maxConcurrency, cacheSyncTimeout)).
		Complete(r)
}

func isValidConfigMap(cm *corev1.ConfigMap) bool {
	if v, ok := cm.Labels[consts.LabelSConfigControllerSourceKey]; ok {
		return v == "true"
	}
	return false
}
